# Capture path audit: hook dispatcher, inbox store, session markers, paths

Scope read in full: `internal/hook/hook.go`, `spawn.go`, `maintain.go` (plus `resume.go` and `trigger.go` in the same package, because capture calls into them), all of `internal/inbox`, `internal/session`, `internal/paths`, `internal/freetext`, `cmd/eva-md/inbox.go`, and the hook entry in `cmd/eva-md/main.go`. Read for context only: `internal/catchup/parser.go` and parts of `catchup.go` (the transcript reader the hook calls), `internal/identity/key.go`, `internal/update/input.go` (how a consumer reads what capture wrote), `internal/health/watchdog.go`, the plugin hook files under `internal/install/testdata`. Design read: `docs/design.md` 81-109, 421-527, 789-798; `docs/known-limitations.md` "Capture, identity and config" and "Removing captured chats"; `docs/decisions.md` 20-119; `docs/verification.md` "Live checks", "Performance", "Not verified yet".

Nothing in the repository was changed, and no test or restricted command was run. All paths below are relative to `the repository`.

Every statement is marked **[verified]** (read in the code at the cited lines) or **[inferred]** (a consequence I reason to, or host behaviour I could not observe here).

---

## 1. How it works

### 1.1 Trigger and process model

- Each host hook event starts one short-lived process: `eva-md hook --host claude-code|codex`, with the payload on stdin. Claude Code registers SessionStart, UserPromptSubmit, PostToolUse, PostToolUseFailure, Stop, StopFailure, PreCompact, PostCompact, PostModelSwitch, SessionEnd; Codex registers SessionStart, UserPromptSubmit, PostToolUse, Stop, PreCompact, SessionEnd, Interrupt. PostToolUse and PostToolUseFailure are marked `async` on both hosts. **[verified]** `internal/install/testdata/claude-hooks.golden.json`, `codex-hooks.golden.json`.
- The Codex command ends in `|| echo '{}'`, so Stop gets valid output with the binary missing. **[verified]** same file.
- `runHook` gives the whole invocation a 1.5 s context, loads the TOML config inside it, and treats a config that exists but cannot be read as "capture nothing" (it forces the `EVA_MD_INNER` guard) with a one-line notice at SessionStart. **[verified]** `cmd/eva-md/main.go:114-149`.
- `hook.Run` always returns 0, recovers panics, never writes a block decision, and writes exactly `{}` for a Codex Stop even when the payload does not parse (a regex finds the event name in raw bytes). **[verified]** `internal/hook/hook.go:110`, `119-145`, `167-170`.

### 1.2 Budgets, caps and time limits

| Limit | Value | Where |
| --- | --- | --- |
| Hook payload | 2 MiB; larger is dropped whole | `hook.go:30`, `167-173` |
| Hook budget | 300 ms; 1.5 s for SessionStart, PostToolUse, PostToolUseFailure | `hook.go:229-235` |
| Output phase reserve | 10 ms before the deadline | `hook.go:151`, `200` |
| Chat-start project recovery | 150 ms | `hook.go:344` |
| Per-event transcript recovery pass | 100 ms, one 1 MiB window | `hook.go:433`; `catchup.go:29`, `386-397` |
| Late-run heartbeat | 100 ms after the output | `hook/resume.go:348` |
| Brief marker reserve | 20 ms | `hook/resume.go:49` |
| Entry text | 200 KB, head 100 KB + tail 100 KB, the cut counted in `trunc` | `inbox.go:24`, `122-130` |
| Metadata fields | 4,096 bytes each; identity fields 512 | `inbox.go:99`, `119` |
| Inbox line a reader decodes | 2 MiB | `inbox.go:25`, `walk.go:17` |
| Tool line | command head and MCP args 200 chars, path or URL 1,024, 64 patch files, 16 MCP args, an MCP value over 1 KiB dropped | `hook.go:806`, `818`, `831`, `962`, `987` |
| Plan line | 100 steps, 2,048 chars each | `hook.go:1048`, `1058` |
| Session lock and inbox flock | non-blocking flock polled every 2 ms until the context ends | `session/lock.go:20-38`; `inbox.go:269-289` |
| Marker value | 64 KiB | `session.go:28` |
| Safe file name | 128 bytes, hash suffix when changed | `paths.go:22`, `248-275` |
| Update triggers | Stop with 60 KB unprocessed; prompt 30 min after the file last changed with 600 bytes unprocessed; SessionStart takes up to 5 sessions idle 2 min | `hook/trigger.go:18-24` |
| Daily maintain | the first hook event of the local day | `hook/maintain.go:55-81` |
| Push retry | at chat starts, at most every 30 min | `hook/maintain.go:23`, `88-105` |
| Heartbeat history | last 20 runs per event | `hook/resume.go:204` |
| `inbox clear` waits | 5 s for a search index, 10 s for a session lock, none for the update lock | `cmd/eva-md/inbox.go:31-34`, `290-296` |

### 1.3 Data flow of one event (`capture`, `hook.go:268-586`)

1. **Read** the payload in a goroutine bounded by the deadline (`hook.go:153-190`).
2. **Guards**, in order: `EVA_MD_INNER=1`; `CLAUDE_CODE_REMOTE=true`; `cwd` not absolute, session id not "safe" (redaction changes it), state dir not absolute; path exclusions checked on the path as written and as resolved; inside the wiki, the state folder or a temp folder; a Codex memory-writing run (`hook.go:281-315`). The folder guards leave only a time stamp in `heartbeats/<host>/uncaptured.json` (`hook/resume.go:280-314`).
3. **Identity**: project key, remote and branch from `cwd` through the identity cache; a failure loses the event and records a failed heartbeat (`hook.go:320-325`). Remote exclusions are checked after it and before any marker write (`326-329`). A desktop "spare" folder stores only the model marker (`335-341`).
4. **SessionStart only**: `catchup.RecoverProject`, 150 ms, recovers other idle sessions of the project from their transcripts (`343-347`).
5. `queueMaintain` asks for the day's maintain and, at a chat start, a push retry (`360`; decided later in `launch`).
6. **Session lock** `locks/<host>-<id>.lock`, held to the end of the event, brief building included (`361-365`).
7. **Markers**: remember the model; read `off_record`; remember the branch as the thread, or read it back when HEAD is detached (`379-409`). An unchanged marker is not rewritten (`591-600`).
8. Build the entry (`422`): `ts` is the hook's clock after the lock is taken (`418`), `cwd` is made home-relative, `origin="hook"`.
9. `catchup.Register` records the transcript path and cursor (`424`).
10. **Record controls** on a typed prompt, before any transcript pass: "off the record" sets the marker, `was_off` and the recovery block, stores an `off_record` event and returns a confirmation; "back on the record" reverses it (`461-493`).
11. While off the record: only the event's fixed name is kept (`494-497`).
12. **Privacy gate**: a 100 ms transcript recovery pass must reach the transcript's end (`caughtUp`, `432-441`). If it does not, the event keeps only its fixed name and gets no brief (`503-505`). The off marker is then read again, since recovery may have set it (`506-512`).
13. **Event switch** (`513-572`):
    - UserPromptSubmit: empty and built-in slash commands store nothing; a synthetic or program-run prompt becomes a `host_prompt` event without text; otherwise `role=user kind=prompt` with the prompt.
    - Stop: `last_assistant_message` as `role=assistant kind=reply`; a missing one becomes `gap: missing final reply`.
    - PostToolUse/Failure: one `tool` or `plan` line from `toolLine` (`781-881`): name, target, exit code. Never output.
    - PostCompact: `compact_summary` as `host_summary`, unless `was_off`.
    - StopFailure, SessionStart, SessionEnd, PreCompact, PostModelSwitch, Interrupt: a fixed event name.
14. **Append** (`573-581`), skipping a prompt or reply that recovery just stored (`catchup.Contains`, only when this pass recovered rows).
15. **Update triggers** (`522-524`, `568-570`, `582-584`) and, for SessionStart and prompts, `resumeContext`, which builds the brief, pointers and recall (`571`, `585`).
16. **Deferred**: the event's heartbeat, an atomic write with fsync (`352-357`; `hook/resume.go:228-257`).
17. Back in `Run`: write the output, then `launch` starts detached jobs (`140-144`). A capture that passes the deadline delivers no output and no spawns, and a late heartbeat marks the run failed (`214-219`, `hook/resume.go:338-351`).

### 1.4 The inbox append (`inbox.go:81-212`)

Redact the text (fail closed: a redactor panic stores the entry without text, `89-92`, `247-257`); reject identity fields that hold a known-format secret (`97-102`); format-only redaction for the branch (`107-112`); redact every other metadata field (`114-121`); cut text over 200 KB (`122-130`); open the inbox root through `os.Root` with no symlink in any path component (`143`; `paths.go:89-137`); create or open the file refusing symlinks, replaced files and hard links (`158-165`, `291-339`); flock (`166`); repair a partial last line left by a killed writer with a newline and a `gap: interrupted append` entry, so physical line numbers stay stable (`176-196`); one `write`; fsync the file and its folder for user and assistant entries only (`203-210`).

Reading: `ReadFrom` returns complete lines after a byte offset, turns a malformed or oversized line into a gap entry and leaves an unterminated last line unread (`359-427`). `Walk` yields searchable lines and tracks the off-the-record state from the `off_record`/`on_record` events in file order (`walk.go:49-90`). `Trusted` accepts an entry only if its host, session and project match the file it sits in (`walk.go:127-144`).

Watermark: `inbox/<key>/.state/<file>.wm` holds the processed byte offset, `more` (the last update pass stopped inside a group) and `wait <size>` (the rest was too small to process), replaced atomically (`inbox.go:443-558`).

### 1.5 What is written where (all under `~/.local/state/eva-md`, folders 0700, files 0600)

| Path | Content | Written by | Durability | Pruned |
| --- | --- | --- | --- | --- |
| `inbox/<project-key>/<host>-<session>.jsonl` | entries | hooks, transcript recovery | fsync on user/assistant | never; `inbox clear` only |
| `inbox/<key>/.state/<file>.wm` | watermark | update | atomic + fsync | with the file |
| `inbox/.removed` | removed session ids | `inbox clear` | flock + fsync | never (`known-limitations.md:104`) |
| `sessions/<host>/<session>/{off_record,was_off,briefed,model,thread,prompts}` | markers | hooks, recovery | atomic + fsync; `prompts` in place | never **[verified: no remover found by grep]** |
| `locks/<host>-<session>.lock` | empty lock file | hooks | none | never **[verified: same grep]** |
| `catchup/<host>/<session>.json` | transcript path, cursor, block | hooks | atomic + fsync (`catchup.go:1216-1234`) | 30 days / newest 2,048 (`catchup/prune.go:22-25`) |
| `heartbeats/<host>/<Event>.json` | last 20 runs, failure flag, duration | every event | atomic + fsync | overwritten |
| `heartbeats/<host>/uncaptured.json` | a time | guarded runs | in place, no fsync | overwritten |
| `commit/attempt-<date>`, `commit/push-retry` | trigger markers | `launch` | none | old ones by maintain |
| `logs/update-<date>.log` | detached jobs' stdout/stderr | `detach` | append | by `update.OpenLog` |

### 1.6 Detached jobs (`spawn.go`, `maintain.go`)

`launch` runs after the output is written. An update job is always started; the day's maintain only by the event that creates `commit/attempt-<date>` with `O_EXCL`; a push retry only when no maintain started and `push-retry` is older than 30 minutes with a push pending (`maintain.go:55-105`). `detach` starts only the installed binary at `<share>/bin/eva-md`, in its own session (`Setsid`), cwd `/`, stdin `/dev/null`, stdout and stderr to the day's log, with the update's allowlisted environment plus `EVA_MD_INNER=1`; maintain also gets the SSH agent and git config variables (`spawn.go:47-90`). It never waits and its error is discarded (`spawn.go:34`).

### 1.7 Removal (`cmd/eva-md/inbox.go`, `inbox/remove.go`)

`eva-md inbox clear --session|--project|--before [--apply]` removes whole session files. It previews by default, refuses an empty filter value, takes the update lock without waiting, holds the search index of the projects concerned, then for each session takes the session lock, forgets the transcript recovery record (unless it keeps the chat off the record), appends the session to `inbox/.removed`, removes the file, then the watermark, and last drops the search index and an empty project folder (`cmd/eva-md/inbox.go:288-373`; `inbox/remove.go:197-309`).

### 1.8 What the tests cover, and do not

Covered **[verified by reading test names and fixtures]**: every event on both hosts stores one entry of the right role and never tool output (`hook_test.go:113-180`); every guard (`182-251`); off the record and compaction summaries (`261-415`); control phrases and synthetic prompts (`418-447`); tool lines (`449-547`); malformed input, panics, budgets and deadlines with injected clocks (`548-1180`); parallel hooks for one session (`1049`); detached jobs outliving the hook (`spawn_test.go:125`); daily maintain and push retry spacing (`maintain_test.go`); heartbeats (`beat_test.go`); concurrent and cross-process appends, partial lines, symlink and hard link refusal (`inbox_test.go`); removal ordering and busy cases (`cmd/eva-md/inbox_test.go`).

Not covered:
- **Real host payloads.** Every hook test builds its payload from the Go `payload` struct (`hook_test.go:49`, `83-93`); `internal/hook` has no `testdata`. Nothing proves the field names, `source` values, prompt wrappers or tool names of a real host version.
- **Ordering** of async tool hooks against Stop on a real host (`verification.md` "A burst of parallel tool calls" is listed as not verified).
- **Completeness** in real use: `verification.md:144` lists "a stretch of everyday use with zero hook errors" as still to do.
- Windows; any host other than the two.

### 1.9 Where the patching has been heaviest

`hook.go` grew from 729 to 1,075 lines in 11 days. The churn: update triggers and off-the-record gating (`54a6dff`, +266/-31), test seams for machine-speed independence (`01cc6bf`, +44/-30: `Slack`, `withDeadline`), 22 bug fixes (`5a8e654`, +28), program-run chats (`bf296c9`, +17/-4), and health telemetry (`51b241c`, +56/-3: run durations, `uncaptured` stamps, `transcript_elsewhere`). The last commit added 2,395 lines across this area, 574 of them heartbeat tests. The transcript reader the hook depends on (`internal/catchup`) is 3,070 non-test lines, against 1,075 for the dispatcher itself. **[verified]** `git log --numstat`, `wc -l`.

---

## 2. What must be kept

Hard-won invariants a redesign must not lose. All **[verified]** at the lines cited.

**Host safety**
1. A hook always exits 0, contains every panic, never emits a block decision, and a Codex Stop always gets `{}` (`hook.go:119-145`, `110`, `167-170`; `main.go:114-119`).
2. A capture past its deadline delivers nothing, spawns included, and 10 ms is reserved to write the output (`hook.go:149-151`, `200-219`).
3. Detached jobs start only after the output is written, hold none of the hook's pipes, run in their own session, and are only ever the installed binary at an absolute path (`hook.go:140-144`; `spawn.go:47-69`).
4. Recursion guard: `EVA_MD_INNER=1` stops capture inside eva-md's own model runs, and every detached job carries it (`hook.go:281`; `spawn.go:43`, `80`).
5. Hooks make no network call and use no model.

**Privacy**
6. Redaction runs before any byte reaches disk and fails closed; identity fields holding a known-format secret reject the whole entry; all other metadata is redacted too (`inbox.go:89-121`, `247-257`).
7. Redact before cutting, so a token across the cut cannot leak a prefix (`hook.go:803-806`).
8. Tool lines never hold tool output, file content or error text; URLs lose user part, fragment and credential-named query parameters; credential-named MCP arguments are dropped (`hook.go:781-988`).
9. Exclusions are checked on the path as written and as resolved, and remote exclusions before any marker is written (`hook.go:305-329`).
10. Off the record: a typed control is honoured before any transcript pass; the state lives in two places (marker and recovery record) so one failed write cannot lift it; `was_off` keeps compaction summaries out; a transcript not read to its end is treated as possibly off the record (`hook.go:461-512`, `538-547`). `inbox clear` keeps a recovery record that alone keeps a chat off the record (`cmd/eva-md/inbox.go:171-174`, `346`).
11. Prompts the user did not type are never stored as the user's words (`hook.go:518-521`, `679-694`, `726-738`).
12. Nothing said off the record reaches a search (`walk.go:77-87`).

**Store integrity**
13. Append-only JSONL under flock with `O_APPEND`; a killed writer's partial line is terminated and marked, so physical line numbers, which refs depend on, stay stable (`inbox.go:176-196`); readers turn a malformed line into a gap entry (`359-427`).
14. JSON escaping means pasted text can never forge an entry boundary or a speaker; on the way to a model, `freetext` quotes any line shaped like a speaker label or like the brief's own notes (`freetext.go:16-87`).
15. Path confinement: every state path is opened through `os.Root` with no symlink ancestor; names from payloads pass `SafeName`; symlinks, hard links and files replaced while opening are refused (`paths.go:89-137`, `248-275`; `inbox.go:291-339`; `session.go:76-125`; `session/lock.go:68-117`).
16. An entry is trusted only in the file its host, session and project name (`walk.go:127-144`).
17. One writer per session: the session lock serializes hooks and transcript recovery (`session/lock.go:14-39`; `hook.go:361`).
18. Watermark replaced atomically, with `more` and `wait` (`inbox.go:443-558`).
19. Removal order: lock, forget recovery record, record in `.removed`, remove file, then watermark; preview by default; an empty filter is a usage error, never "all" (`cmd/eva-md/inbox.go:82-103`, `288-373`).

**Operability**
20. Latency targets (capture p95 under 50 ms, brief under 300 ms) and the benchmark that checks them.
21. Tests independent of machine speed: injected clock, `Slack`, `withDeadline`, hang guards (`hook.go:45-64`; `hook_test.go:52-74`).
22. Per-event run history with failure flag and duration, and the "hook ran but captured nothing by design" stamp, so the doctor can tell a dead hook from an excluded folder (`hook/resume.go:228-314`).
23. Unchanged markers are not rewritten, which saves fsyncs (`hook.go:588-600`).

---

## 3. Where it falls short of the goal

Ranked by consequence for "the best memory, and invisible".

### capture-1 (memory-quality, critical): capture is lossy at the source, and the loss cannot be undone

- **Evidence [verified].** The hook stores, per turn: the typed prompt, the final reply only (`last_assistant_message`, `hook.go:528-535`), and one line per tool call with name, target and exit code (`hook.go:781-881`). Transcript recovery stores the same two things: a Claude assistant row only when `stop_reason` is `end_turn` or `stop_sequence` (`catchup/parser.go:131-134`), a Codex message only with `phase == "final"` (`parser.go:169`, `189`). Edit and Write keep the path, not the change (`hook.go:809-819`). A failure keeps an exit code, not the error (`862-879`). A subagent's prompt becomes `host_prompt` with no text (`518-521`, `680-682`); no SubagentStop hook is registered. Text over 200 KB loses its middle (`inbox.go:122-130`). `design.md:1391` states the choice: "Assistant narration between tool calls and all tool output are not saved."
- **What it costs.** The model that organizes memory never sees why something was done, what was tried and failed, what the error was, or what a subagent found, unless the final reply happens to restate it. Error-to-fix and root-cause knowledge is the most valuable memory for a coding agent and the hardest to work out from the code. eva-md keeps no copy of the host transcript, so a better extractor next year cannot go back for it **[inferred: hosts prune their own transcripts on their own schedule]**.
- **Direction.** Make capture two-tier. Tier 1 is a full-fidelity, redacted, local event log per session, normalized from the host transcript (every assistant message, tool call, bounded tool result digest, subagent report), with a retention policy and compression. Tier 2 is the distilled view a model reads, which can be regenerated. Tool output stays out of anything that leaves the machine; a bounded, redacted failure digest goes in.
- **Action: rebuild** (the capture model). Touches decision 16 "Tool activity ... without their output", so it needs the owner's agreement. Not in known-limitations.

### capture-2 (architecture, high): two ingest paths reconciled by hand, and a privacy gate that makes every hook wait on a transcript parse

- **Evidence [verified].** The hook stores from payloads; `internal/catchup` stores the same prompts and replies from transcripts (`origin=catchup`), and the two are reconciled by turn id or text hash (`hook.go:573-581`; `catchup.go:1479-1498`). Every event runs a 100 ms recovery pass first, and unless it reaches the transcript's end the event keeps only its fixed name (`hook.go:432-441`, `498-505`). Then: a prompt or reply is deferred to a later pass, a tool line or compaction summary is dropped with no gap marker, and a chat start or prompt gets no brief of any kind (`known-limitations.md:42`). A pass reads one 1 MiB window (`catchup.go:29`, `386-397`), so a turn that writes more than that to the transcript puts the next events behind **[inferred: a pasted image or several large tool results would do it]**. A payload over 2 MiB is dropped whole (`hook.go:171-173`).
- **What it costs.** The weakest capture is in tool-heavy agentic sessions, the main use. The reconciliation and gating logic is the largest and most patched part of the capture side (3,070 lines in catchup).
- **Direction.** One ordered ingester per session. Hooks become cheap signals ("session S moved; transcript at P") plus the few facts a transcript lacks. A single writer reads transcript rows in order, evaluates the privacy state machine in the stream, and emits idempotent events with stable ids. Host adapters hide payload and transcript formats. The off-the-record invariant becomes a property of ordered ingestion, not a gate on every hook.
- **Action: rebuild.** Partly known (`known-limitations.md:42`).

### capture-3 (recall-quality, high): the agent only "remembers" when capture succeeds, in the same process and budget

- **Evidence [verified].** `resumeContext`, which returns the brief, the refresh, the system pointers and recalled knowledge, is the last call of `capture` (`hook.go:571`, `585`). Everything before it must succeed within 300 ms (1.5 s at SessionStart): identity (`320-325`), the session lock, shared with async tool hooks of the same chat (`361-364`), four marker reads and writes (`379-409`), the recovery pass (`503-505`). Each failure path returns `""`. A deadline miss delivers nothing (`214-219`). None of this is said in the chat.
- **What it costs.** Under load, lock contention or a slow transcript, the one visible benefit (context at chat start, recall on a prompt) silently disappears, and the user sees an agent that forgot. How often is unmeasured (see capture-8).
- **Direction.** Separate the read path from the write path. Context injection reads a precomputed per-project and per-session context pack (one small file, built by the background job), takes no session lock and does not depend on ingestion. Capture is a spool append. Each has its own budget and its own failure accounting.
- **Action: rebuild** (dispatcher structure).

### capture-4 (reliability-safety, high): "the user typed this" is decided by a deny-list that defaults to yes

- **Evidence [verified].** `synthetic()` treats a prompt as not typed only if it carries an agent id, one of 12 `source` strings, or one of 8 text prefixes (`hook.go:679-694`); `headless()` knows three entrypoint values (`732-738`). Anything else is stored as `role=user kind=prompt`, "your words, verbatim" (`525-527`). Transcript recovery uses the opposite rule, an allowlist (`catchup/parser.go:116-120`). Codex `exec` runs cannot be told apart (`known-limitations.md:27`). The wrappers and entrypoints have not been seen in a real payload (`verification.md` "Host signals").
- **What it costs.** A new host wrapper, a teammate or scheduler message, or text a hook injected is stored with the user's authority. User words are what standing rules and knowledge are promoted from, so this is a path for stored text to steer later chats **[inferred for the promotion step, which is outside this part]**.
- **Direction.** A first-class provenance field on every event (typed, pasted, host-injected, agent, program, unknown), assigned by an allowlist: unknown is never "typed". Promotion rules downstream key on it. Contract tests from recorded payloads per host version keep the allowlist honest.
- **Action: rebuild** the classification; **add** the field. Partly known (lines 27-28).

### capture-5 (architecture, high): entries have no identity, no order key and no structure, and refs are line numbers in a local file

- **Evidence [verified].** `Entry` has no id or sequence (`inbox.go:28-45`). `ts` is the hook's clock after it wins the session lock (`hook.go:361`, `418`), at one-second resolution, while recovered entries carry the transcript's own timestamp (`parser.go:58`). Tool hooks are async and take the lock by polling (`session/lock.go:20-38`), so file order is arrival order. Consumers read in file order; `turn` is used only for dedupe in catchup, and `agent`, `origin`, `red` and `trunc` have no reader anywhere (grep over non-test code). The 200 KB cut joins head and tail with no marker in the text (`inbox.go:129`). A tool call is flattened to `"<Tool> <detail> (exit N)"` and parsed back by string cuts in the updater (`update/input.go:512-523`, `621-623`). A ref is `s:<first 8 chars of session>#<line>` (`inbox.go:585-591`).
- **What it costs.** Tool lines can land after the reply of their turn or in the next turn **[inferred from async hooks]**. Refs break or silently point at different text after a removal and re-capture (`known-limitations.md:101`), and do not resolve on another machine. Provenance is what lets memory be checked and corrected, and it rests on the least stable identifier available.
- **Direction.** Inbox schema v2: turn-structured records with a stable event id (session + monotonic sequence from the single writer), typed tool records, explicit truncation markers, refs by id. Promoted facts carry a short redacted evidence excerpt so provenance survives removal and travels with the wiki.
- **Action: rebuild** (store schema).

### capture-6 (memory-quality, high): the store is partitioned by a project key that changes

- **Evidence [verified].** A session file lives at `inbox/<project-key>/<host>-<session>.jsonl` (`inbox.go:52-69`). The key is the normalized remote, or `local-<folder>-<hash of the absolute path>` (`identity/key.go:13-36`), and also depends on the current redaction rules (`key.go:38-51`). The brief, search and the chat-start catch-up look in that folder. No code in `internal/inbox` or `internal/identity` moves or aliases a folder (grep for alias, rekey, migrate).
- **What it costs.** Adding a remote to a local repo, moving or renaming its folder, renaming or transferring the repo, cloning through an SSH alias, or an upgrade's rule change each start a new folder; earlier chats drop out of the brief and of project search with no notice **[inferred for remote-add, move and rename; the rule-change and alias cases are in `known-limitations.md:20-21`]**. A new project that starts local and is later pushed is the common case.
- **Direction.** Store sessions by their immutable id. Make "project" an attribute resolved through a project registry that holds aliases (remotes, root paths, the repo's root commit), so re-keying is a registry edit and never a data move.
- **Action: rebuild** (layout) and **add** (registry). Partly known.

### capture-7 (invisibility, high): background work has no scheduler; it rides on whichever hook happens to fire

- **Evidence [verified].** Jobs are started only by `launch` at the end of a hook (`spawn.go:19-36`). The day's maintain is due only for the event that creates `commit/attempt-<date>`; the marker is created before the spawn and the spawn's error is discarded (`maintain.go:55-81`; `spawn.go:27-34`). A failed or declined commit is not retried that day; only a pending push is, at chat starts, every 30 minutes (`maintain.go:23`, `88-105`; `health/background.go:386-390`). An update is asked for only at a Stop with 60 KB unprocessed, a prompt 30 minutes after the last write, SessionEnd, PreCompact, or a later chat start in the same project (`trigger.go:18-24`). Every such Stop spawns a process, with no backoff state at the trigger (`maintain.go:63`).
- **What it costs.** How fresh memory is depends on the user chatting again in the same project on the same Mac. A desktop chat left open under 60 KB is not organized until the next prompt half an hour later or the next chat. One missed signing approval costs a day of commit. No chat, no maintenance.
- **Direction.** A durable job ledger (one record per job with state, attempts and next-try time) that any eva-md entry point drains, with "N minutes idle after the last event" as the main update trigger; and an on-demand OS trigger, a launchd agent with a start interval or a watched path, which is not a resident server.
- **Action: rebuild** (`spawn.go`, `maintain.go`, `trigger.go` into one scheduler). Touches the ruled-out "resident servers".

### capture-8 (evaluation, high): nothing measures whether capture is complete, ordered or correctly attributed

- **Evidence [verified].** Hook tests use payloads built from the Go struct (`hook_test.go:49`, `113-180`); there are no recorded host payloads. The benchmark measures latency only. A heartbeat marks a run failed on a panic, an error or a timeout (`hook/resume.go:206-257`) and the doctor fails only when half or more of the last 20 runs failed (`health/watchdog.go:27-38`). The drift alarm fires only when recovery recognizes no row at all (`known-limitations.md:44`). Real-use capture is listed as not verified (`verification.md:144`).
- **What it costs.** A 10 to 40 percent loss of tool lines, a host version that renames a field, or prompts misfiled as typed would all pass every check. The rebuild decisions above rest on rates nobody has.
- **Direction.** A capture audit: replay scrubbed real transcripts and payloads, per host version, through the adapters and score turn coverage, tool-call coverage, order agreement, provenance accuracy and off-the-record leaks, in CI. The same audit as a doctor mode compares a real session's transcript with its inbox.
- **Action: add.**

### capture-9 (reach, high): two hosts, one operating system, one machine, one person, and stdin as the only way in

- **Evidence [verified].** Hosts are string literals in the dispatcher, the trust check, the file-name parser and the recovery store (`hook.go:146`; `walk.go:128`; `remove.go:55`; `catchup.go:1158`). The payload struct is the union of both hosts' fields, and tool parsing branches on the host (`hook.go:79-108`, `862-875`). Locks and spawning use `flock`, `Setsid` and `O_NOFOLLOW` (`session/lock.go:24`; `inbox.go:274`; `spawn.go:64`), so a Windows build is not possible **[inferred]**. `CLAUDE_CODE_REMOTE=true` captures nothing (`hook.go:284`). There is no ingest command or write tool: a surface without hooks cannot contribute. The inbox, markers and refs are per machine, and no code pulls the wiki (grep for pull or fetch in `internal/wikigit`). A symlinked home captures nothing and no chat start says so (`known-limitations.md:24`).
- **What it costs.** Claude Desktop's chat tab, cloud sessions, the Codex VS Code extension, a second machine and a team are all outside what capture can serve.
- **Direction.** A host adapter interface (payload to canonical event, plus declared capabilities); an ingest API, as a CLI command and an MCP write tool, for surfaces without hooks; an OS layer for locks and spawning; a machine id on every event so logs from several machines can merge.
- **Action: add** (adapters, ingest API); **rebuild** the host-specific parts of `hook.go`. Partly known (decision 3).

### capture-10 (reliability-safety, medium): off the record is a magic phrase, matched loosely in one direction and strictly in the other

- **Evidence [verified].** `RecordControl` fires when a prompt starts with "off the record" followed by anything, or when any line of the prompt is only the phrase, pasted text included (`session/control.go:13-34`). The same holds for "back on the record", so a pasted line turns capture back on. "please go off the record today" does not fire (`hook_test.go:419`). The confirmation reaches the user as a `systemMessage` on Claude only; on Codex it goes to the model's context (`hook.go:752-763`). It is not retroactive, and removal is whole-session only.
- **What it costs.** A false positive silently stops memory for the rest of the chat. A false negative, or a pasted "back on the record", captures what the user believed was private. Both are surprises.
- **Direction.** An explicit, unambiguous control with visible state: a slash command or skill and an MCP tool, with the phrase kept as a strict whole-prompt convenience; a user-visible confirmation on both hosts; a retroactive "forget the last N turns".
- **Action: evolve.**

### capture-11 (architecture, medium): per-session state is spread over eight places, and health telemetry is paid for on the hot path

- **Evidence [verified].** One session's state lives in the session file, its watermark, six marker files, a lock file, a recovery record, the heartbeats, the identity cache and the search index (table in 1.5), each with its own format, atomicity rule and pruning rule, or none: marker folders, lock files and `.removed` are never pruned. Consistency across them is kept by ordering conventions in comments; `inbox clear` needs four locks and a six-step order (`cmd/eva-md/inbox.go:273-373`). Every event, each async tool call included, ends with an atomic heartbeat write, which fsyncs the file and its folder (`hook/resume.go:256`; `paths.go:166`, `178`); on macOS Go's `Sync` is `F_FULLFSYNC` (Go source, `internal/poll/fd_fsync_darwin.go`). The code's own comment puts an atomic save at about 11 ms (`hook/resume.go:275-276`), against a measured capture of 25 to 30 ms. Heartbeat history is read-modify-write with no lock, so concurrent runs lose each other's record (`hook/resume.go:316-318`). Each state access re-walks the path from `/` (`paths.go:89-137`).
- **What it costs.** No user-visible failure today; it is the reason each new feature needs a new marker file, a new ordering rule and a new set of tests, and why full disk flushes run on every tool call for telemetry that, by the code's own account, costs "at most a false alarm" to lose.
- **Direction.** One session record and one append-only log per session under one lock, or an embedded transactional store if the two-dependency rule is relaxed. Health becomes append-only event lines aggregated off the hot path. Every artifact gets a defined lifecycle.
- **Action: rebuild** (storage layer; keep the confinement and atomic-write primitives in `internal/paths`).

### capture-12 (memory-quality, medium, known): forgetting is coarse, manual and does not cascade

- **Evidence [verified].** Removal is whole-session, from the command line, by session id, with no `inbox list` (`cmd/eva-md/inbox.go:62-145`; `known-limitations.md:100`, `103`). The JSONL is never rewritten, so a secret that redaction missed stays until the whole chat goes, and a later, better redaction rule never reaches lines already stored (`inbox.go:71-78`). The wiki and its git history keep what was derived (`known-limitations.md:100`). There is no retention limit (`design.md:483`).
- **What it costs.** "Forget that" cannot be done from the chat, cannot be done for one line, and does not reach the wiki facts built on it.
- **Direction.** Immutable segments with a compaction pass that applies tombstones and the current redaction rules; cascade through event ids to derived wiki items (mark them retracted); an in-chat forget control; a stated retention policy.
- **Action: evolve** (after capture-5's ids exist). Known.

---

## 4. Essential and accidental complexity

**Essential**: fail-closed hooks inside a hard time budget; redaction before disk; path confinement against a same-user process planting links; an off-the-record boundary that survives a missed hook; append-only storage with crash repair; two hosts whose payloads and transcripts differ and drift.

**Accidental**: two ingest paths reconciled after the fact; a recovery pass and a privacy gate on every event; structured data flattened to text and parsed back; per-session state in eight places with hand-kept ordering; durable health telemetry on the hot path; background work triggered by marker files from whichever hook fires; host differences handled by string switches across four packages.

## 5. Overall call

**Rebuild the capture pipeline around a canonical event model; keep the primitives.**

Keep as they are: `internal/paths` (confinement, atomic write, `SafeName`), `internal/freetext`, the redaction call pattern in `inbox.append`, `session.Lock`, the detached-spawn recipe in `spawn.go`, the guard list and the fail-closed shell of `hook.Run`, the removal protocol's ordering, and the test discipline.

Rebuild: the dispatcher's capture body (split into signal, ingest and context-read paths), the inbox schema and layout (event ids, turn structure, session-keyed storage, project registry), the trigger and scheduling code, and the host-specific parsing behind adapters.

Add: provenance field, ingest API, capture audit.

The reason: findings 1, 2, 3, 5, 6 and 9 are the same root cause seen from six sides. The stored unit is "one line per hook event, in arrival order, in a folder named by a mutable key", and each later need (recovery, ordering, privacy, refs, other hosts) was added as a compensating mechanism around that unit.

## 6. Open questions

1. Will the owner revisit decision 16 ("tool activity ... without their output") and the ruled-out "resident servers", to allow a local full-fidelity log and an on-demand launchd trigger?
2. Is the two-dependency rule open, to allow an embedded transactional store?
3. What do real payloads look like per host version: `agent_id`, `source` values, prompt wrappers, Codex tool names? No recorded payload exists in the repo.
4. Do the hosts guarantee any order between async PostToolUse hooks and Stop, and does Codex wait for async hooks?
5. How long does each host keep its transcripts, and may eva-md keep a redacted normalized copy?
6. Does Codex show `additionalContext` to the user, or only to the model (the off-the-record confirmation depends on it)?
7. What are the real loss rates (tool lines dropped behind recovery, deadline misses, payloads over 2 MiB)? The audit in capture-8 should run before the rebuild's scope is fixed.
8. Are several machines and a team in scope for the inbox, given "the inbox is never committed"?
9. Which hookless surfaces matter first (Claude Desktop chat, cloud sessions), and what can each expose: MCP only, or an export?

---

## Verification

A second reader tried to refute each finding against the code at commit `51b241c`. Read in full: `internal/hook/hook.go`, `spawn.go`, `maintain.go`, `trigger.go`, `resume.go`, `recall.go`, all of `internal/inbox`, `internal/session`, `internal/paths`, `internal/freetext`, `cmd/eva-md/inbox.go`. Read in part: `internal/catchup/catchup.go` and `parser.go`, `internal/identity/key.go`, `internal/health/watchdog.go`, `health.go` and `background.go`, `internal/update/input.go`, `validate.go`, `rules.go`, `schema.go` and `job.go`, `internal/resume/read.go`, `internal/install/templates.go`, and the four docs. Nothing in the repository was changed, and no test or restricted command ran. Host behaviour could not be observed here; where a point rests on it, it says so.

### Verdicts

| Finding | Verdict | Their severity | Mine |
| --- | --- | --- | --- |
| capture-1 | confirmed | critical | high |
| capture-2 | partly | high | medium |
| capture-3 | partly | high | medium |
| capture-4 | confirmed | high | high |
| capture-5 | partly | high | medium |
| capture-6 | confirmed | high | high |
| capture-7 | partly | high | medium |
| capture-8 | confirmed | high | high |
| capture-9 | partly | high | medium |
| capture-10 | confirmed | medium | medium |
| capture-11 | confirmed | medium | medium |
| capture-12 | confirmed | medium | medium |

No finding is refuted: every cited line shows what is claimed. Five are scoped down, because the impact as written goes past what the code shows or leaves out something that already handles part of it.

### capture-1: confirmed, high rather than critical

- Every cited line holds: `hook.go:528-535`; `hook.go:781-881` (a Bash line keeps only the first line of the command, 200 characters, at `805-806`); `catchup/parser.go:131-135` (an assistant row is kept only at `end_turn` or `stop_sequence`, and only when all its content is text, since `textContent` returns "" on any other item, `239-246`); `parser.go:169`, `189`; `hook.go:518-521`; `inbox.go:122-130`.
- It is a recorded choice with a stated reason, not an oversight: `decisions.md:99` and `design.md:1391` ("tool output is where secrets and noise live").
- "Permanent" holds only once the host prunes its transcript. eva-md keeps the transcript's path and a cursor (`catchup.go:61-92`) and by rule never reads back before the first hook (`catchup.go:1-2`; "History backfill" is ruled out). Host retention was not checked.
- Why high: prompts, final replies and tool lines are enough for the goal's own test ("where did we stop?", `decisions.md:7`). What is lost is error-to-fix knowledge.
- The cost side belongs in the decision: the proposal puts tool output in a store whose redaction still misses about 1% of hard passwords (`known-limitations.md:7`) and that never expires (`design.md:483`).

### capture-2: partly, medium

- Shown: the gate (`hook.go:432-441`, `498-505`), one window a pass (`catchup.go:386-397`), dedupe (`hook.go:573-581`; `catchup.go:1479-1498`). A tool event held back appends nothing, because `eventName` gives "" for it (`hook.go:657-675`), and `failed` stays false.
- Not shown: "in tool-heavy sessions events fall behind". Each event runs its own pass, so a tool-heavy chat gets more passes. A pass is behind only when over 1 MiB of rows arrive between two events, or 100 ms is not enough. The one figure in the repo is "Capture with transcript recovery: under 50 ms" (`verification.md:102`). No rate exists either way (see missed-3).
- The gate is narrower than "every hook": it holds an event only on a timeout, a pending window or a failed privacy write. Every other recovery error counts as caught up (`hook.go:437-440`): no record, a missing or replaced transcript, an identity mismatch.
- "Most patched" is not in the history: `internal/catchup` has 7 commits and 64 removed lines, `internal/hook` 12 and 226.
- `known` should be true for the symptoms: `known-limitations.md:42` lists them, as section 3 of this report says.
- Risk in the direction: the single ingester takes the transcript as its source. The code treats transcripts as the fallback, because their format is not a stable interface (`design.md:91`, `1391`), Codex may name none (`design.md:526`) and a moved host folder has none (`known-limitations.md:50`). The hook payload fields are the documented contract.

### capture-3: partly, medium

- Shown: every cited line. The session lock is also held while the brief is built (`hook.go:365`, then `571` and `585`).
- A brief lost at a chat start is retried: with no briefed marker, the next prompt gets the startup brief (`hook/resume.go:130-134`; `design.md:580`). What is lost for good is that one prompt's pointers and recall.
- "Nothing reports it" is half right. Identity, lock, marker and deadline failures are recorded as failed runs (`hook.go:323`, `351-357`, `218`), and a chat start says so once half of the last 20 failed (`health/health.go:108-112`). The gate path records a success (missed-3).
- A failed inbox append does not stop the brief (`hook.go:578-585`).
- Capture measures 25 to 30 ms against a 300 ms budget (`verification.md:99`), so the budget is not tight in the ordinary case.
- Constraint on the direction: the brief is withheld behind the gate on purpose (`design.md:503`, `1030`), and the brief's own state, the `briefed` and `prompts` markers, is written under the session lock (`hook/resume.go:74-79`). A read path with no lock needs a new rule for both.

### capture-4: confirmed, high

- Shown: `hook.go:679-694` (12 sources and 8 prefixes, counted), `732-738`, `525-527`.
- The promotion step, marked inferred, is in the code. The validator takes a claim as the user's only by role `user` and kind `prompt` (`update/validate.go:264-274`), a standing rule is added only on that (`update/rules.go:137`), and the schema tells the model the same (`update/schema.go:24-28`). Capture's attribution is the validator's only ground.
- Recovery's allowlist is Claude's only (`parser.go:120`). Every Codex `user_message` is typed (`parser.go:161-166`).
- The two paths carry different lists: 8 prefixes in the hook, 13 and 9 wrappers in recovery (`parser.go:285`, `322`). The same text can be the user's by one path and dropped by the other.
- Paste marking exists for Claude only (`update/validate.go:266`; `update/paste.go:5-17`), so on Codex pasted text counts as the user's own words.
- For the redesign: the default was chosen so an unknown entrypoint keeps capture working (`hook.go:728-729`). An allowlist must go on storing the text, with provenance unknown, or a new host surface stores none of the user's words.

### capture-5: partly, medium

- Shown: `inbox.go:28-45`; `hook.go:361`, `418`; `agent`, `origin`, `red` and `trunc` have no reader outside tests, and `turn` is read only by recovery's dedupe (grep over non-test code); `update/input.go:512-523`; `inbox.go:585-591`.
- "The least stable identifier" overstates it. The file is append-only and never rewritten, and a killed writer's partial line is ended so that line numbers hold (`inbox.go:176-196`). A ref moves only after a removal and re-capture, or when 8 characters collide; both are documented and partly handled (`known-limitations.md:63`, `101`, `108`).
- Not resolving on another machine follows decision 4 (the inbox is never committed), not a fault of refs.
- Tool lines landing after the reply is inferred. It needs a tool hook slower than the model's final reply.
- Added evidence for the direction: `Store.ReadFrom` has no caller outside tests. The update (`update/input.go:57`, `153`), the brief (`resume/read.go:196-200`, `347`), search (`resume/search.go:594-597`), the index (`inbox/walk.go:78-81`) and recovery's dedupe (`catchup.go:1391-1431`) each decode the JSONL, and the first four each track off-the-record ranges on their own. A schema v2 changes five readers, not one.

### capture-6: confirmed, high

- Shown: `inbox.go:52-69`; `identity/key.go:13-36`, `38-51`; no alias or move code in `identity`, `inbox`, `resume` or `update`.
- The wiki splits too. A project's folder is accepted only when its now page names the same remote, and a local project only under its own slug (`resume/read.go:613-635`). A local repo that gains a remote loses its thread pages and its project-scoped recall rows with its inbox folder.
- It is decision 10 (`decisions.md:64`). The remote-added and folder-moved cases are not in known-limitations.

### capture-7: partly, medium

- Shown: `spawn.go:19-36`; the marker made before the spawn, whose error is dropped (`maintain.go:55-81`; `spawn.go:34`); `health/background.go:386-390`; `trigger.go:18-24`. The comment at `spawn.go:17-18`, "the next trigger tries again", is true for an update and false for the day's maintain, whose marker then exists.
- "The same project" is wrong for the backlog. A person's first chat start of the day, in any project, takes the day's sweep: 5 idle sessions from the whole inbox (`update/job.go:132-141`; `hook/trigger.go:46-60`; `design.md:815`).
- SessionEnd and PreCompact ask for an update too (`hook.go:568-570`), so a CLI chat that is closed is organized at once. The gap is a chat left open.
- A missed signing approval delays the commit and push, not the pages. Pages are written when the update runs (`decisions.md:55`), and the brief reads the local wiki.
- The brief fills an update's gap with raw inbox entries by design (`decisions.md:50`).
- Conflict in the direction: `design.md:1360` rules out "daemons or sleeping jobs", beside `decisions.md:113`. A launchd agent on an interval needs that reopened.

### capture-8: confirmed, high

- Shown: hook tests marshal the Go struct (`hook_test.go:83-93`); no `testdata` under `internal/hook` or `internal/catchup`; `health/watchdog.go:27-38`; `verification.md:144`.
- Three coarse signals do exist: failed runs at half of 20, the drift alarm for a total failure (`known-limitations.md:44`) and capture freshness for hooks that don't fire at all (`health/watchdog.go:137`), beside one-off live checks by hand (`verification.md:68-74`). None measures coverage, order or attribution.

### capture-9: partly, medium

- Shown: the literals at the four places; `hook.go:79-108`; `spawn.go:64`; `hook.go:284`; no ingest command (`cmd/eva-md/main.go:50-84`); no pull or fetch in `internal/wikigit`.
- The host literal is in 11 packages and 22 files, not four packages: `cmd/eva-md`, `catchup`, `config`, `health`, `hook`, `inbox`, `install`, `mcp`, `resume`, `systempage`, `update`.
- These are declared scope, so `known` should be true: `decisions.md:32-36` and `design.md:1359` exclude the Chat tab, cloud sessions and the Codex extension; `decisions.md:28` keeps one wiki a machine; macOS only is stated policy.

### capture-10: confirmed, medium

- Shown: `session/control.go:13-34`; `hook_test.go:419`; `hook.go:752-763`.
- When a prompt holds both phrases, off wins, since the loop checks it first. On Claude the confirmation is shown, so the silent case is Codex.
- The phrase is decision 16 (`decisions.md:101`). missed-2 below is a wider gap in the same guarantee.

### capture-11: confirmed, medium

- Shown: no code removes a session's marker folder or lock file (grep over `Sessions()` and `Locks()`); `hook/resume.go:256` leads to `paths.go:166` and `178`; Go's `Sync` on macOS is `F_FULLFSYNC` (`internal/poll/fd_fsync_darwin.go:22` in Go 1.26); `hook/resume.go:275-276`.
- The heartbeat runs in capture's deferred call, before capture returns (`hook.go:352-357`), so its save comes before the hook's output on every prompt and chat start. The lock is released first, so the save does not lengthen the lock.
- Heartbeats are kept per host and event, not per session.

### capture-12: confirmed, medium, known

- Shown: `cmd/eva-md/inbox.go:62-145`; `known-limitations.md:100-108`; `design.md:483`.
- One correction. "Newer redaction rules never reach stored lines" is true of the file only. The brief, show, search and the update input redact stored text again with the current rules on every read (`freetext/freetext.go:66-67`; `resume/resume.go:605`; `resume/read.go:710`; `resume/search.go:961`, `972`; `update/input.go:654`). The compaction pass is needed for the bytes at rest, not for what a model or a person is shown.

### Missed

**missed-1 (memory-quality, high): what the user decides through the host's own tools is never captured.**
- Evidence. The Claude matcher names 12 tools and `mcp__` (`install/templates.go:74-75`); the question tool, the plan-approval tool and the subagent tool are not among them, and `toolLine` keeps only the name of a tool it doesn't know (`hook.go:855-861`). Recovery drops a user row that holds a tool result (`catchup/parser.go:271-272`) and an assistant row that ends in a tool call (`parser.go:131-134`). So the answer to a question, the text of an approved plan and a subagent's report reach the inbox by no path. That these arrive as tool input and result, not as a prompt, is host behaviour, not observable here.
- Cost. The update files a decision as stated or accepted only on a cited user prompt (`update/validate.go:264-274`). A choice made in the question dialog, or a plan approved there, can never be the user's decision in the wiki.
- Direction. Capture these tools as typed events: the question with the chosen answer, marked as chosen by the user; the plan text with approved or rejected; a subagent's final report as the agent's. Promotion treats a chosen answer as accepted.
- Action: add. Not in known-limitations.

**missed-2 (reliability-safety, medium): off the record is tied to the session id, not to the conversation.**
- Evidence. `off_record` and `was_off` are files under `sessions/<host>/<session id>/` (`session/session.go:164-178`), read by the payload's id (`hook.go:390`, `541`). The plugins register SessionStart for `fork` (`install/templates.go:72`), and `design.md:566-568` says a forked chat arrives with the conversation loaded. Nothing copies either marker to a new id: at a chat start `was_off` is cleared or left as it is, never inherited (`hook.go:563-567`). No test in `internal/hook` or `internal/catchup` mentions a fork. That a fork gets a new session id is host behaviour, not observable here.
- Cost. A fork of a chat that is or was off the record is captured from its first turn, and its compaction summary, which restates the whole context by the code's own comment (`hook.go:539-540`), is stored. The user said nothing to turn capture back on.
- Direction. Carry the privacy state along the conversation's lineage: a resume or fork start inherits `off_record` and `was_off` from its parent session, and a start whose parent is unknown is treated as `was_off`.
- Action: evolve. Not in known-limitations, whose line 41 covers only a resumed chat that keeps its id.

**missed-3 (evaluation, medium): an event the gate holds back is recorded as a successful run, and recovery's reasons for falling behind have no reader.**
- Evidence. `markersOnly` returns without setting `failed` (`hook.go:454-459`, `503-505`), so the heartbeat records a success. Recovery writes `tail_limit`, `slow_row` and `oversized_row` into the session's record (`catchup.go:390`, `410`, `642`), and nothing in health, the doctor or status reads them (grep). `ErrPending` is read only at `hook.go:440`.
- Cost. The loss rate that decides capture-2 and capture-3 cannot be read from any installed machine, though the hook knows each time it happens.
- Direction. Add to the heartbeat's run record whether the run was held back and what it cost (a deferred prompt or reply, a dropped tool line, a withheld brief), and a doctor row for it. A week of real use then gives the rate before the rebuild's scope is fixed.
- Action: add.

### Also noted, not ranked

- The hook path redacts a prompt or reply whole before it cuts it (`inbox.go:89`, then `122`), up to the 2 MiB payload limit, inside the 300 ms budget, with no way to stop it. Recovery bounds the same work (`catchup.go:870-883`) because, by its own comment, 200 KiB "can outrun a 100 ms hook budget". By that figure a paste of several hundred KiB would make the prompt's hook miss its deadline: no brief or recall for that prompt, and the text waits for recovery. This was not measured.
- Before the rebuild's scope is fixed, the order that follows from the above is: missed-3 and capture-8 first, since they give the rates; then capture-4, capture-6 and missed-1, which hold whatever the rates are.
