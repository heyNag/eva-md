# The record: capture, ingestion and redaction (L0 Ledger)

Design section for the eva-md v2 plan. Area: the canonical event model and inbox schema v2, the capture contract, the hook floor and the detached ingester, the off-the-record guarantee, session-keyed storage with a project registry, per-session state, retention and forgetting, the redaction engine, and the capture audit.

Finding ids used below: `capture-N` and `capture-mN` (capture.md and its missed list), `catchup-N` and `catchup-mN` (catchup.md F1 to F12 and the two its verifier added), `redact-N` and `redact-mN`, `brief-N` and `brief-MN`, `search-N` and `search-mN`, `setup-N`, and from neighbouring audits `mcp:capture-1`, `mcp:provenance-1`, `mcp:echo-1`, `mcp:subagents-1`, `update-model-4`, `update-model-11`, `update-model-M3`, `storage:evidence-1`, `storage:replay-1`. Every audit's verification section was read, catchup's and the brief's at the end of this work, once they had been written. No finding this design rests on was refuted. Several were scoped down, and the design follows the second readers where they differ from the first: capture-2, capture-3, capture-5 and capture-7 are medium, catchup-1 is medium, redact-1 is high and narrower than first stated, and the rates behind capture-2 and catchup-2 are unknown.

Everything here is a proposal. Numbers given as bars are targets; the baseline they are measured against does not exist yet and is the first work package.

---

## 1. Goal and the measurable bar

**Goal.** One local, redacted, ordered record of every chat that later stages can trust: each event has a stable id, a known author, a known order and an honest account of what was cut. A hook never waits on anything but its own small write. Nothing said off the record is ever readable by any consumer. A project keeps its memory through a rename, a first push or a moved folder. Forgetting works for one line and reaches what was derived from it.

**The bar.** All of these are measured by the capture audit (section 11) on the fixture corpus in CI, and the starred ones also on the real install.

| Measure | Bar | Today |
| --- | --- | --- |
| `otr_leaks`: text stored at or after an off-the-record control, under every hook-drop schedule the audit generates | 0 (hard gate) | not measured |
| `pv_false_typed`: events stored as the user's typed words that the fixture labels otherwise | 0 (hard gate) | not measured; deny-list defaults to yes (capture-4) |
| `turn_coverage`*: turns in the host's record whose prompt and final reply are in the ledger | at least 99.5% | not measured (capture-8) |
| `tool_coverage`*: tool calls in the host's record that have a ledger record | at least 99% | not measured; tool lines are dropped while a pass is behind (capture-2, catchup-2) |
| `decision_capture`: dialog answers and plan decisions in the fixtures that are in the ledger with the right author | 100% | 0% (capture-m1) |
| `order_agreement`: adjacent pairs in a turn in the host's order | at least 99.9% | arrival order (capture-5) |
| `dupes` | 0 | possible (catchup-9) |
| Hook time, capture*: p95 with a 16 KiB reply, and with a 64 KiB reply | under 50 ms, under 120 ms | under 50 ms measured with a 23-byte reply (redact-4) |
| Events lost or reduced to a name because ingestion was behind* | 0 | unknown rate (capture-m3) |
| `identity_continuity`: scripted rename, first push, moved folder, linked worktree keep one project id | 100% | 0% (capture-6, setup-3) |
| `oracle_violations`: a labelled true secret the frozen baseline removes that the engine keeps | 0 (hard gate) | 0 by construction (union) |
| `fact_loss_rate`: labelled non-secret auth facts masked | at most a fifth of v1's rate, if the owner agrees to decision LG-D2 | every listed case masked (redact-1) |
| `hex32_kept_rate`: random 32-hex tokens after a trigger word kept whole | under 1% | 19% (redact-m2) |
| `forget_residue_bytes`: bytes of forgotten text left in the ledger and the search index | 0 | whole-session removal only (capture-12) |

Hook latency on a Mac and the two coverage rates have no v1 baseline. Stage 0 produces it (LG-01, LG-02) before the scope of the rebuild is fixed, as the capture audit's verifier advised.

---

## 2. The design in one page

```
host hook event ──► eva-md hook ──► host adapter (payload → canonical events)
                      │  guards, typed privacy control, redact, append
                      ▼
            sessions/<host>/<id>/spool.jsonl      (redacted, held, unreadable to consumers)
                      │  signal: "session moved"
                      ▼
            ingest job (detached, single writer per session, no deadline, no model, no network)
              reads host record (transcript) through a versioned adapter, in order
              runs the privacy state machine in the stream
              reconciles spool + record, assigns provenance, order and seq
                      ▼
            sessions/<host>/<id>/events.jsonl     (the ledger: released, ordered, stable ids)
            sessions/<host>/<id>/state.json       (the one session record)
            projects/<pid>/catalog.json           (derived session catalog)
            registry.json                         (projects and their aliases)
                      ▼
            one reader API ──► derive, deliver, search index, store (excerpts), audit
```

Six rules carry the design.

1. **One unit, one writer.** The stored unit is a typed event in a session, written to the ledger by one ingester per session, in the host's order. Hooks never write the ledger.
2. **Hooks are the floor.** A hook stores what its payload holds, redacted, in the session's spool, and returns. The hook payload fields are the documented contract on both hosts; the transcript is not. If no transcript can be read, the spool alone still becomes a correct, smaller ledger.
3. **Held until released.** What a hook stored is invisible to every consumer until the ingester releases it into the ledger. The ledger holds only released events, so no reader needs a "stop at the verified point" rule of its own.
4. **Order makes privacy.** The ingester reads the host's record in order and evaluates record controls in the stream. An event is released only when every user-typed row before it has been checked. A control found late deletes what was held after it. Off the record stops being a gate each hook waits on and becomes a property of release.
5. **Project is an attribute.** Sessions are stored by their immutable host and session id. The project is resolved through a registry with aliases; re-keying is a registry edit, never a data move.
6. **Ids, not line numbers.** A reference names a session and a sequence number that survives repair, forgetting and migration. Files may be rewritten by one maintenance step only, and that step keeps every id.

What is deliberately not here: no embedded database, no new dependency, no second copy of the host transcript, no model anywhere in this layer, no network, and nothing committed or synced. The ledger stays on the machine that captured it.

---

## 3. The canonical event model (inbox schema v2)

### 3.1 Files of one session

All under `~/.local/state/eva-md/ledger/sessions/<host>/<session>/`, folders 0700, files 0600, opened through `paths.OpenRoot` with `SafeName` on both name parts, as today.

| File | Content | Writer | Durability |
| --- | --- | --- | --- |
| `events.jsonl` | header line, then released events in order | the ingester, under the session lock | flock, `O_APPEND`, fsync per batch |
| `spool.jsonl` | events a hook stored, redacted, not yet released | hooks (flock, `O_APPEND`), truncated by the ingester | fsync for prompts, replies and controls, as v1 does for user and assistant entries |
| `state.json` | the one session record (3.5) | hooks only to create it or to change privacy; otherwise the ingester | atomic replace with fsync (`paths.WriteFileAtomicAt`) |
| one file per neighbouring area, by a fixed table | for example deliver's context model | that area | its own |

The session lock (`session.Lock`, kept) guards `state.json` and `events.jsonl`. A hook takes it only when it creates the record or changes privacy. A tool event takes no lock but the spool's flock.

### 3.2 Header and trust

The first line of `events.jsonl` is `{"v":2,"k":"header","host":"claude-code","session":"…","machine":"m_…","created":"…"}`. A reader trusts a file only when its header's host and session are the folder's (the v1 rule that an entry is trusted only in the file that names it, capture must-keep 16). Events do not repeat host and session.

### 3.3 Event fields

| Field | Type | Meaning |
| --- | --- | --- |
| `seq` | int | sequence in the session, assigned at release, never reused, kept through repair. The id of an event is (host, session, seq) |
| `t` | RFC 3339 UTC, milliseconds | the source's own time: the host row's when there is one, else the hook's clock at process start (not after a lock, capture-5) |
| `turn` | int | ordinal of the turn in the session, from 1; 0 before the first prompt |
| `tid` | string | the host's turn id (`prompt_id`, `turn_id`) when given |
| `k` | enum | kind, table 3.4 |
| `pv` | enum | provenance: who authored the text (3.6) |
| `text` | string | redacted text; absent for kinds that have none |
| `red` | list of `[start,end,kind,tier,handle]` | where markers stand in `text`, with their kind, tier and an optional keyed handle (10.5). Replaces v1's count, which nothing read (redact-6) |
| `rv` | int | redaction rule version that produced `text` (redact-3) |
| `cut` | list of `{at,lost,why}` | every truncation: byte position in `text`, bytes lost, and why (`size`, `deadline`, `policy`). The text also carries one visible line at the cut, `[… N bytes cut by eva.md …]` (capture-5) |
| `agent` | string | subagent id when the event belongs to one |
| `taint` | list | what the turn touched: `web`, `net`, `mcp`, `memory`, `subagent` (3.7). On replies, segments and reports only |
| `src` | `{h,r,key}` | `h`: 1 when a hook supplied it; `r`: the host row id when the record supplied it; `key`: the dedupe key |
| kind-specific | | table 3.4 |

Context that changes rarely is not repeated on every event. A `ctx` event carries `project` (registry id), `branch`, `cwd` (home-relative), `head` (commit, read by the ingester when the turn settles, update-model-4) and `model`; each event belongs to the last `ctx` before it. The reader hands every event out with its context filled in.

### 3.4 Kinds

| `k` | Text | Extra fields | Author (`pv`) | Source |
| --- | --- | --- | --- | --- |
| `prompt` | the prompt | `paste`: spans the host marked as pasted | `typed`, `pasted`, `program`, `host`, `unknown` | hook, record |
| `reply` | the final assistant message | `end`: `done`, `interrupted`, `failed:<type>` | `agent` | hook, record |
| `segment` | assistant text between tool calls | | `agent` | record only |
| `tool` | none, or the failure digest | `tool`, `cls` (`read`, `edit`, `run`, `search`, `fetch`, `mcp`, `delegate`, `memory`, `other`), `call` (host tool-use id), `target` (`cmd` head, `paths`, `pattern`, `url`, `args`), `st` (`ok`, `fail`, `denied`, `interrupted`, `unknown`), `exit`, `self` (eva-md's own lookup) | `tool` | hook, record |
| `dialog` | question, options and the answer, in fixed labelled lines | `answer_pv`: `chosen` or `typed` | `agent` for the question, the answer by `answer_pv` | hook, record |
| `plan_decision` | the plan text | `decision`: `approved`, `rejected`, `unknown` | `agent` for the text; the decision is the user's | hook, record |
| `tasks` | none | the task list by id: `[{id,subject,status}]`, the whole list each time | `agent` | hook |
| `sub_task` | what a subagent was asked | `agent`, `agent_type` | `agent` | hook |
| `sub_report` | the subagent's final report | `agent`, `agent_type` | `agent` | hook, record |
| `summary` | the host's compaction summary | `trigger` | `host` | hook (Claude), record (Codex) |
| `delivery` | none | `surface` (`start`, `prompt`, `tool`, `subagent`, `compact`), `items`: `[{id,hash}]`, `bytes`. Ids and hashes, never text | `eva` | deliver, through `AppendDelivery` |
| `control` | none | `op`: `off`, `on`, `forget`; `by`: `prompt`, `command`, `tool`, `late` | `typed` or `eva` | hook, ingester |
| `ctx` | none | 3.3 | `eva` | ingester |
| `life` | none | `ev`: `start:<source>`, `end:<reason>`, `compact:<trigger>`, `interrupt`, `stop_failure:<type>`, `model_switch` | `host` | hook |
| `gap` | none | `why`, `lost` (bytes, when known) | `eva` | either |
| `gone` | none | a tombstone: the kind it replaced, `why`, `at` | `eva` | forget (9) |

A `tool` record is typed fields, never a formatted line that a consumer parses back (capture-5, brief-10).

### 3.5 The session record (`state.json`)

One file replaces the watermark's neighbours, six marker files, the lock file's implicit state, the recovery record and the per-session part of the heartbeats (capture-11).

```json
{"v":1,"host":"claude-code","session":"…","machine":"m_…","created":"…","updated":"…",
 "project":"p_k3v9…","binding":{"root":"~/src/app","remote":"github.com/acme/app","branch":"main","cwd":"~/src/app","detached":false},
 "entry":{"entrypoint":"cli","program":false,"host_version":"2.1.321","profile":"claude/2026-11"},
 "privacy":{"state":"on","was_off":false,"by":"","lineage":"known","parent":""},
 "record":{"path":"…","dev":0,"ino":0,"cursor":0,"state":"verified","reason":"","seen":"…"},
 "ledger":{"gen":1,"next_seq":42,"rv_min":6,"held":0,"oldest_held":""},
 "manifest":{"opening":{"seq":1,"head":"first 200 characters, redacted"},"last_work":"…","turns":7,"tools":31,"fails":2,
             "branches":["main"],"files":["a.go","b.go"],"tasks":[{"id":"1","subject":"…","status":"done"}],
             "last_check":{"cmd":"go test ./...","st":"ok","seq":38},"ended":"open"},
 "audit":{"hook":49,"matched":47,"hook_only":2,"record_only":5,"dupes":0,"gaps":0,"unknown_pv":0,"held_ms_max":220},
 "legacy":[{"key":"github.com__acme__app","lines":120,"map":"identity"}],
 "removed":false}
```

`record.state` is one of `verified` (read and recognized through `cursor`), `behind` (readable, an ingest is due), `unavailable` (no path, missing, outside the host's store, compressed or not local), `unrecognized` (the adapter's canary failed). One explicit state per session, and one policy for each (6.4), replaces a gate that is strict when recovery is slow and absent when it resets or fails (catchup-8).

`manifest` is what the brief today re-derives from up to 64 MiB of raw JSONL at every chat start (brief-4). `last_work` moves only on a prompt, reply, tool or task event, so an event never makes a chat the newest (brief must-keep 8).

What other areas keep per session lives beside this file under the same lifecycle (created with the session, removed with it, pruned with it). The update's progress is not here: it is a range of `seq` in derive's own job ledger (contract C-D3), which also answers storage:replay-1, since a sequence range is something a store can record and a re-cloned wiki can be replayed from.

### 3.6 Provenance, assigned by allowlist

`pv` says who authored the text. It is assigned in the adapter from positive evidence only. Anything not positively identified is `unknown`, and `unknown` text is still stored, because an allowlist that dropped it would store none of the user's words on a new host surface (capture-4, its verifier's note).

| `pv` | Positive evidence required |
| --- | --- |
| `typed` | a prompt event from a hook whose entrypoint is on the adapter's list of interactive entrypoints, with no agent id, no listed source value, no host wrapper at its start, and, where the record is readable, a counterpart row that the record's own flags mark as the user's (Claude: its own prompt id, human origin, not meta, not sidechain, not a compaction summary). One list of wrappers, in the adapter, used by both sources: today the hook has 8 prefixes and recovery 13 and 9, and they disagree (capture-4, catchup-6) |
| `pasted` | a prompt that is wholly a host-marked paste. A typed prompt with pasted parts stays `typed` and lists the parts in `paste`. Only Claude marks pastes; the Codex adapter declares `paste_marking: false`, and derive decides what that means for promotion |
| `chosen` | the answer in a `dialog` event picked from options the agent wrote, or a plan approval. The user endorsed text they did not write |
| `program` | prompts of a chat a program runs (the entrypoint list), messages from another session, teammate messages |
| `host` | compaction summaries, scheduled-task and loop prompts, task notifications, bash-mode input and output, interrupt notices, command banners |
| `agent` | assistant replies and segments, subagent tasks and reports, plan text, dialog questions |
| `tool` | tool records and failure digests |
| `eva` | deliveries, controls eva-md wrote, context and gap events |
| `unknown` | everything else |

### 3.7 Taint, set by code

`taint` is not authorship. It records what the turn touched before the text was written, computed by the ingester from the turn's typed tool records: `web` (fetch and search tools), `net` (a shell command whose first word is on a small table: curl, wget, gh, glab, http clients), `mcp` (a tool of a server other than eva-md's), `memory` (a delivery record in or before the turn, or an eva-md lookup, `self`), `subagent`. This is the input the derive area needs for its taint rules (update-model-11) and for telling restated memory from new evidence (mcp:echo-1, mcp:provenance-1).

### 3.8 References

- Full id: `<host>:<session>#<seq>`. Display form: `e:<first 8 of session>#<seq>`, ranges as `e:5f0c9a1e#40-44`.
- The v1 form `s:<sid8>#<line>` resolves for good, through the session's `legacy` map (section 14). A wiki written by v1 needs no rewrite.
- A reference names an event, never a byte offset or a line. What travels with the wiki is a short excerpt with the event id and a hash of the excerpt (contract C-S2); the ledger itself never leaves the machine (storage:evidence-1, search-7).

---

## 4. The capture contract

### 4.1 What eva-md keeps

| Kept | Bound | Why | Decision needed |
| --- | --- | --- | --- |
| Prompts and final replies | 200 KB, head and tail, with a marked cut | as v1 | none |
| Typed tool records without output | command head 200 characters, path or URL 1,024, 64 patch files, 16 MCP arguments, as v1 | as v1, now as fields | none |
| Dialog answers and plan decisions | question 1 KB, 8 options of 200 characters, answer 2 KB, plan 16 KB | the user's decisions made through the host's own tools reach memory by no path today (capture-m1, mcp:capture-1, update-model-M3) | none: these are the conversation |
| The task list by id | 50 tasks, subject 200 characters | "Latest plan" is one task's status line on Claude (brief-10) | none |
| Host compaction summaries | 32 KB; never once the chat has been off the record | as v1; adds Codex from its record | none |
| Delivery records | ids and hashes | what eva-md put into a chat leaves no trace today (mcp:provenance-1) | none |
| Failure digests | only on a failed call: for `run` and `edit`, the first 1,024 bytes and the last 512 of the error text, cut after redaction under the strictest policy (10.3); for other classes the first line, 200 characters | error-to-fix knowledge is lost at the source (capture-1) | LG-D1: changes "tool activity without output" |
| Subagent task and report | task 4 KB, report 16 KB | delegated work is invisible (mcp:subagents-1) | LG-D1 |
| Assistant text between tool calls (`segment`) | 8 KB each, 64 KB a turn | reasons and rejected options are in a file eva-md already reads (capture-1, catchup-1); the only capture that cannot be made later, since hosts prune transcripts | LG-D1 |

The three rows under LG-D1 are switched by one config key, `capture.detail = "full" | "decisions" | "minimal"`. Segments are captured into the ledger, but the plan should not let derive read them until an evaluation shows they improve what is extracted: the record is cheap and local, the derived memory is what must stay small.

### 4.2 What eva-md never keeps

- The output of a tool call that succeeded; file contents read or written; diff and patch bodies; fetched pages and search results; attachment and image bytes.
- Model reasoning blocks; system prompts, tool definitions and the request records Claude Code transcripts now hold.
- Text injected by other plugins' hooks. eva-md's own injections are kept as ids only.
- Anything while a chat is off the record but fixed event names; anything in an excluded path or remote, the wiki, the state folder or a temporary folder; eva-md's own model runs (`EVA_MD_INNER`); a host's memory-writing runs.
- Credential-named MCP arguments; a URL's user part, fragment and credential-named query parameters (v1 rules, kept).
- Anything from before the first hook, unless the owner allows the bounded backfill of LG-D5.

### 4.3 Order of work on text

Redact, then cut, then store (capture must-keep 7). A failure digest is redacted whole before its head and tail are taken.

---

## 5. The hook: the cheap floor

### 5.1 What one hook run does

1. Read the payload (2 MiB limit), in the fail-closed shell of `hook.Run`, unchanged: always exit 0, contain panics, never a block decision, `{}` for a Codex Stop, 10 ms reserved for output, detached work only after the output is written (capture must-keep 1 to 5).
2. Guards, unchanged and in the same order: inner run, cloud session, unsafe ids, path exclusions on the path as written and as resolved, wiki, state and temporary folders, a host's memory-writing run, then identity and remote exclusion before any state is written.
3. Read `state.json` without a lock. If it is missing, create it under the session lock with the project binding. The identity resolver and its cache are kept; the registry lookup is two map reads (8.2).
4. The adapter turns the payload into canonical events. No transcript is read.
5. Privacy. A typed prompt is checked for a record control before anything else (7.2). If the session is off, only the event's fixed name is appended.
6. Redact each text with a deadline (10.4), append to the spool under its flock, fsync for prompts, replies and controls.
7. Append one content-free line to the hook health log (5.3).
8. Write the output. Then signal the ingest job (6.1).

Removed from the hook: the transcript pass and its gate, four marker reads and writes, the heartbeat's atomic save with a full disk flush on every tool call, the project catch-up at chat start, update triggers decided from file sizes. Delivery (the brief, recall) is a separate read path owned by the deliver area with its own budget and failure accounting; a capture failure never suppresses delivery, and delivery never waits for ingestion (capture-3). Delivery reads `privacy.state` and stays silent while a chat is off the record, as today.

### 5.2 A text too large for the budget

The engine returns what it scanned when its deadline ends (10.4). The hook stores the redacted prefix with a `cut` of reason `deadline`. When the host's record is readable the ingester replaces it with the full text, redacted without a deadline. When it is not, the cut stands and is counted. Nothing is ever stored unredacted to be cleaned later: redaction before any byte reaches disk stays an absolute rule (capture must-keep 6).

### 5.3 Hook health without a hot-path flush

One line per run, appended without fsync to `ledger/health/hooks-<day>.jsonl`: time, host, event, duration, outcome (`ok`, `late`, `fail:<class>`, `uncaptured`), and whether a text was cut for the deadline. A guarded run in a folder eva-md does not capture writes time, host and `uncaptured` only, never the event or session, as the v1 stamp does. Aggregation is the operations area's, off the hot path. Lines per event let it see one event that fails every time, which pooled histories hide. This keeps capture must-keep 22 and removes the 11 ms save and its lost-update race (capture-11).

### 5.4 Budget

The limits stay 300 ms, and 1.5 s at SessionStart and for tool events. The project's target, capture p95 under 50 ms, stays for the benchmark's standard payload and now has to hold at a realistic size too. The benchmark gains payload sizes: `capture_16k_p95_ms` under 50, `capture_64k_p95_ms` under 120, `tool_event_p95_ms` under 25, `session_end_p95_ms` under 200 as today.

---

## 6. The ingester

### 6.1 Process model

`ledger.Ingest(session)` is a job with no model, no network and no deadline slices. The derive area's worker runs it at top priority, single-flight per session (contract C-D1). Until that worker exists, the hook starts `eva-md ingest --session <host>:<id>` with v1's detached-spawn recipe (own session, no inherited pipes, the installed binary by absolute path, `EVA_MD_INNER=1`). Either way it never runs inside a hook.

Signals that ask for it: Stop, SessionEnd, PreCompact, PostCompact, SubagentStop, Interrupt, and a prompt (which also settles the previous turn's stragglers). A tool event does not, so a burst of tool calls starts no burst of processes. An ingester that finds the session lock held leaves a request mark and exits; the running one makes one more pass. A hook that appends to a spool also touches `ledger/dirty/<host>-<session>`; any later ingest run sweeps dirty sessions older than two minutes, so a chat that ended in a crash is ingested by the next hook of any chat, or by the worker's timer if the owner allows one (catchup-3).

### 6.2 One pass

Under the session lock:

1. Load `state.json`, pick the adapter profile by the host version the payloads and the record name.
2. **Locate and trust the host record**, with v1's rules moved into the adapter: only a path a hook named, absolute, named for the session, inside the host's store where its symlinks lead, a regular singly linked file opened without following a link, device, inode and size checked each pass, and for Codex an opening row that names this session and folder (read whole, without the 64 KiB limit that fails long instructions). The store root comes from the host's own environment at registration (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`), not a fixed path (catchup-11).
3. **Read forward from the cursor** with a plain line reader and `encoding/json`. Rows over 8 MiB go through v1's streaming field extractor (`rowScan`, kept as code, without its saved state), so a control beside a huge attachment is still seen.
4. **Classify each row** into zero or more canonical events, carrying the row's own id, time, folder and branch. v1's classification rules move here unchanged where they still apply (which rows are typed prompts, which are final replies, synthetic placeholders, API-error rows, compaction summaries), with their tests. New: mid-turn assistant text, tool calls with their result status, dialog and plan tools, sidechain rows as subagent events. A row that mixes text with an image or a document keeps its text, with one marker line for each attachment left out; v1 drops such a row whole and still counts it as recognized (catchup-m2).
5. **Run the privacy state machine** on typed user rows, in order (7.3).
6. **Reconcile** with the spool (6.3), assign `pv`, `turn`, `taint`.
7. **Redact** what came from the record, with no deadline; spool events are already redacted.
8. **Release** per 6.4: append to `events.jsonl` with `seq`, write `ctx` events when project, branch, folder or model changed (a row is filed by its own folder and branch, so the cursor never jumps over unread rows as v1's baseline reset does, catchup-8).
9. Save `state.json` (cursor, manifest, audit counters), truncate the consumed part of the spool, refresh the project's catalog, and tell the worker which turns settled (contract C-D2).

A crash between steps is safe: every event has a dedupe key (the host row id and an index, or for hook-only events a hash of turn id, kind and tool-use id or text), and the ingester refuses a key the ledger's tail already holds. Appends are idempotent by construction, which removes the text-hash keys, the 1 MiB dedupe window and the double reply (catchup-9).

A row that cannot be decoded, or whose redaction panics, becomes a `gap` event and the cursor moves on: no single row stops ingestion (catchup must-keep).

### 6.3 Reconciliation

For each turn the ingester pairs spool events with record events by turn id and kind, and tool events by tool-use id (or name and a hash of the input when the host gives none).

- Order and row ids come from the record. Fields the record lacks or that are the documented contract come from the hook: the final reply as the host delivered it, the failure text of a tool call, the compaction summary, the entrypoint, the model. Where both sides hold a text, the hook's payload wins and the record fills only what no hook delivered. v1 does the opposite on an ordinary Stop: the transcript's copy of the reply is stored and the host's own `last_assistant_message` is dropped, so the reply of every turn rests on the unstable parser (catchup-m1). A difference between the two texts is counted, never resolved in the record's favour.
- A spool event with no counterpart is kept as it is (`src.h` only). A record event with no counterpart is kept (`src.r` only): that is a missed hook, recovered.
- Per turn the counts on both sides go into `state.audit`. Hook and record report the same turns independently, so a host version that renames a field shows as disagreement at once, on the user's machine and in CI, in place of a drift alarm that fires only when no row at all is recognized (catchup-6, catchup-7).

### 6.4 Release rules

| Rule | Statement |
| --- | --- |
| R1 order | The ledger of a session is written by one writer in the host's order. Where there is no readable record, order is: by turn in first-seen order; inside a turn the prompt, then other events by hook receipt time, then the reply |
| R2 hold | Nothing in a spool is readable by any consumer |
| R3 release | With a readable record: an event is released when the scan has passed its own counterpart row, or, for an event with no counterpart, when the scan has passed a row written after the event's receipt or reached the end of a file modified after it. Reading to the end of the file is not enough by itself, because the host writes its transcript asynchronously |
| R4 no record | With a record that is `unavailable` or `unrecognized`: an event is released when its turn's prompt was itself seen by a hook, since that hook checked the prompt for a control. An event of a turn whose prompt no hook saw is reduced to its fixed name and counted |
| R5 late control | A control found by the scan deletes the held events after it. Under R3 none of them was released. If one was (only possible through R4), it is tombstoned and the forget cascade runs (9.3) |
| R6 exclusion | Before release the ingester checks the session's folder and remote against the current exclusions. An exclusion added after a chat started stops its release (catchup-8) |
| R7 no time bomb | Held events are never released or dropped by the passing of time. If no ingest runs, they wait, and the operations area reports sessions whose oldest held event is older than ten minutes |

R4 is stricter than v1 in one corner (v1 stores freely when there is no transcript) and is the only place the design gives up content for the guarantee.

### 6.5 Adapters and versions

The ledger needs this much of a host adapter; the hosts area owns the interface as a whole.

```go
type Capture interface {
    Host() string
    DecodeHook(raw []byte, env Env) (HookEvents, error) // pure, no I/O, used inside the hook
    Locate(sig Signal, home string) (RecordRef, RecordState)
    Open(ref RecordRef, cursor int64) (RowReader, error)
    DecodeRow(row []byte, st *RowState) (events []Event, recognized bool)
    Caps() Caps // paste_marking, tool_use_ids, reply_in_stop, summary_in_hook, parent_in_start, …
}
```

A profile is chosen by the version the host itself reports. An unknown newer version uses the newest profile and a canary: if rows of the expected types stop being recognized, the session goes `unrecognized` and falls back to the hook floor. The fix for drift is then a release, but capture keeps working and nothing is misfiled meanwhile.

Hosts are no longer string literals in eleven packages (capture-9): every reader gets the host from the session header and the adapter registry.

---

## 7. Off the record

### 7.1 The guarantee

Nothing a user says after an off-the-record control, up to the next on-the-record control, is stored beyond fixed event names, by any path, whatever hooks were missed, and no consumer ever reads text that later turns out to be off the record.

### 7.2 Controls

| Control | Fires on | Change from v1 |
| --- | --- | --- |
| Off, by phrase | a typed prompt (`pv` typed) that starts with "off the record" | pasted text and host-written rows no longer fire it: today a `<bash-stdout>` row with a line that reads "off the record", such as a heading in this repo's own design doc, takes a chat off the record silently (catchup-10) |
| On, by phrase | a typed prompt that is only "back on the record" | strict: today any line that is only the phrase turns capture back on, pasted text included (capture-10) |
| Explicit command | `/eva-md:off-the-record` and `/eva-md:on-the-record` (a plugin command on Claude Code, a skill on Codex), recognized by the hook from the prompt, with no model in the loop; `eva-md record off|on|status`; an MCP tool for surfaces without commands | new |
| Forget | `eva-md forget --session S --last N` and the same as a tool (9.2) | new: retroactive, which v1 has no way to do |
| Late | a control the scan finds in a typed row no hook reported | Off is honoured from that row. On is never honoured late, as today |

The asymmetry is deliberate. A false Off costs memory and is visible; a false On stores what the user believed private.

**Visible state.** Every Off and On is confirmed to the user on both hosts (`systemMessage`; the Codex docs say it is shown as a warning, to be confirmed on a real host, section 13), not only on Claude. A late Off is said once at the next prompt and in status, with the way back. A resumed chat that is off says so once at its start.

### 7.3 State machine

Per session: `on` or `off`, plus `was_off`. Driven only by control events in ledger order.

- Off sets `off` and `was_off`. Written in two places so that one failed write cannot lift it: a `control` event appended to the spool first, then `privacy` in `state.json`. If the state write fails, the ingester sees the control in the stream and repairs the state; if the spool write fails, hooks see the state. If both fail the hook returns no confirmation, and the user sees none.
- While `off`, hooks store fixed names only, and the ingester moves its cursor past rows without storing them.
- A control is checked on every typed row of the session, whatever folder the row was written in. v1 returns before the check for a Claude row whose folder differs from the registered one and honours it on Codex (catchup-8, its verifier's addition).
- A late control can only delete what is still held, which is why release waits for the scan (R3). v1's store is append-only and its updates may already have read the text, so "remove what was captured after it" has no mechanism there (catchup-2, its verifier's caution).
- On clears `off`. The ingester resumes storing at the row of the prompt that carried the control, so nothing between the two controls can be recovered later. This is v1's "move the cursor to the end" rule, now a consequence of order.
- `was_off` keeps compaction summaries out until a startup or clear begins a new context, as today.
- **Lineage** (capture-m2). A start with source `resume`, `fork` or `compact` for a session that has no record yet inherits `privacy` from its parent when the adapter can name the parent. When it cannot, the session starts with `was_off` true: its turns are captured, its compaction summaries, which restate the inherited context, are not.
- A removed or pruned session keeps a stub record whose privacy state and cursor stay, so removal never re-enables capture (catchup must-keep).

---

## 8. Storage, identity and the catalog

### 8.1 Layout

```
~/.local/state/eva-md/ledger/
  registry.json            projects and aliases (program-owned, atomic replace, registry lock)
  sessions/<host>/<session>/{events.jsonl, spool.jsonl, state.json, …}
  projects/<pid>/catalog.json   derived; rebuilt from session records at any time
  dirty/<host>-<session>        empty marks: a spool has content
  tombstones.jsonl              content-free record of what was forgotten
  health/hooks-<day>.jsonl      content-free hook run lines
```

### 8.2 The project registry

```json
{"v":1,"projects":[
  {"id":"p_k3v9x2ab7q1m","kind":"repo","display":"acme/app",
   "remotes":["github.com/acme/app","github.com/acme/app-old"],
   "roots":["~/src/app"],"root_commits":["4b825dc…"],
   "legacy_keys":["github.com__acme__app","local-app-9f2c1a"],
   "wiki_slug":"acme-app","merged_into":"","related":[],"first_seen":"…"}]}
```

- **The id is minted once**, random and opaque. It does not depend on the remote, the path or the redaction rules, so a rules upgrade can no longer move a project (redact-12), and a readable name is display data, redacted separately.
- **Evidence** is what can change: remotes (normalized as today, plus a small table of host forms, such as `ssh.github.com` for `github.com`), root paths, root commits (read by the ingester with one hardened git call, never in a hook, and never from a shallow clone), and v1 keys.
- **Resolution** in the hook, from the identity resolver's answer: by remote; else by root path; else by the v1 key of the folder (`legacy_keys`); else mint. A git failure other than "not a repository" still fails closed and never falls back to another project (setup must-keep 20).
- **Automatic aliasing**, done by the ingester, only when two kinds of evidence agree:

| Case | Evidence | Action |
| --- | --- | --- |
| First push: a local repo gains a remote | same root path, same root commit | add the remote to the project |
| Remote renamed or moved in the same clone | same root path, same root commit, new remote | add the remote |
| Folder moved or renamed, no remote | new path, same root commit, old path gone | add the root |
| Linked worktree of a local repo | same git common directory | same project (v1 gave each worktree its own key) |
| A managed worktree on a detached HEAD | common directory and remote | same project |
| Cloned again under a new remote and a new path | root commit only | a new project, with `related` set. A fork and a rename cannot be told apart offline, so nothing is merged. `eva-md project merge A B` records `merged_into`, and readers follow it |

- **Folders outside git** each get a project of kind `folder`, matched by exact path only. v1 files every such chat under one `general` project, so a chat in one folder is shown the newest chat of any other (brief-7). Matching by ancestor is not done: one chat started in the home folder would swallow every folder under it.
- **Travel.** The wiki holds a project identity record (id, display, remotes, root commits, legacy keys, slug; never local paths), kept by the store area (contract C-S1). On a second machine the registry is seeded from it, so the same repository resolves to the same id.

### 8.3 The session catalog

`projects/<pid>/catalog.json` lists the project's newest 256 sessions with their manifest summaries and a total count. It is rewritten when a session of the project settles a turn. It is a cache: `ledger.RebuildCatalog` rebuilds it from the session records, and deleting it loses nothing. With it a chat start reads one small file and at most one session's tail (brief-4).

### 8.4 Lifecycle of every artifact

| Artifact | Created | Pruned |
| --- | --- | --- |
| session folder | first hook | never while it holds events; a removed session's stub after 400 days unless it carries a privacy state |
| spool | first event | emptied at each release |
| catalog | first settle | rebuilt at will |
| dirty mark | spool append | removed when the spool is empty |
| health lines | each run | files older than 30 days |
| tombstones | forget | never: content-free, a line per forget |

v1 never prunes marker folders, lock files or `.removed`.

---

## 9. Retention, tombstones, forgetting, repair

### 9.1 Retention

| Class | Default | Config |
| --- | --- | --- |
| Conversation: prompts, replies, dialogs, plan decisions, summaries, controls, deliveries, tool records without digests | no time limit, as v1 | none |
| Working detail: segments, failure digests, subagent tasks and reports | 180 days, then the text goes and the event stays as a stub with its id | `retention.detail_days`, 0 keeps for good |
| Spool | until released | none |

Expiring working detail needs the owner (LG-D3): v1 rules out automatic deletion. The richer record is where the verifier's caution applies (redaction misses about 1% of a hard password set, and the v1 store never expires), and a bound on the riskiest class is the proportionate answer.

### 9.2 Forgetting

`eva-md forget` generalizes `inbox clear`. Selectors: `--session`, `--project`, `--before` (v1's three), plus `--event <ref or range>`, `--last N` turns of a session, and `--value` (read from standard input, never an argument: every stored occurrence of that exact text is masked). It previews by default, an empty selector value is a usage error and never means "all", and `--apply` is required, as today.

Order of an applied forget, each step safe to repeat:

1. Take the session lock. Refuse while a derive job holds the session's range.
2. Append the intent to `tombstones.jsonl`: event ids, reason, time. No content.
3. Drop the session's documents from the search index (the index is a cache; removal is immediate there, as today).
4. Rewrite `events.jsonl` into the next generation (9.4): each forgotten event becomes a `gone` stub with its `seq`.
5. Notify derive and store with the event ids (9.3).
6. For a whole session: keep the stub record with its cursor at the record's end and its privacy state, so the transcript cannot bring the chat back.

### 9.3 The cascade

The ledger tells; the store acts (contract C-S3). For the forgotten event ids the store returns the items whose evidence cites them. An item all of whose evidence is gone is marked retracted and leaves every rendered view; an item with other evidence keeps it and loses the excerpt. Excerpts copied into the wiki from those events are removed. The wiki's git history still holds what was pushed, and the command's preview says so in plain words, as v1's does.

### 9.4 Repair: the one rewrite

`events.jsonl` is append-only between repairs. `ledger.Repair(session)` writes a new generation to a temporary file, fsyncs, and renames it over the old one under the session lock, bumping `ledger.gen`. It applies, in one pass: tombstones; expiry; and the current redaction rules to every event whose `rv` is older (redact-3). It never changes a `seq`. The search index sees the new generation and re-indexes that session.

Repair can only remove. A fact a past rule masked wrongly is gone; better precision helps from the day it ships. The plan should say this plainly.

This changes a stated v1 property ("the JSONL is never rewritten", and line numbers stay stable) and needs the owner (LG-D4). The property that mattered was that references stay valid, and ids keep that.

---

## 10. The redaction engine

### 10.1 What is wrong and what is not

Not wrong: the rules. The verifier ran 150,000 random inputs and found the union idempotent and never weaker than the baseline, and linear in size. The several hundred keep and remove cases in the tests are the asset.

Wrong: the contract. Strings in, strings out, two engines at run time, their spans recovered by string alignment, a loop of up to 16 passes times 4 rounds, no way to stop it, no tiers, and three gates that are the same detector with less context each time (redact-3, redact-4, redact-5, redact-m1).

So the engine's driver is rebuilt and its rules are kept, then changed one at a time under a measured corpus. A rewrite of 3,500 lines of heuristics from scratch would lose knowledge the tests only partly pin; more patches to the rules cannot fix the cost or the contract.

### 10.2 The contract

```go
type Finding struct{ Start, End int; Kind, Rule string; Tier Tier; Handle string }
type Result  struct{ Clean string; Findings []Finding; Through int } // Through < len(Clean) when the context ended
func Scan(ctx context.Context, text string, p Policy) Result
func Apply(r Result, p Policy) (out string, marks []Mark)
func Redact(text string) (string, int) // kept: Apply(Scan(…, AtRest)), for callers not yet moved
const Version = 6
```

One pass: clean the text (as today: every format character and escape sequence removed before matching); find candidates in one sweep of the lines with a keyword table; judge each line by its structure; merge spans; replace once. Markers already in the text are atoms no span may enter or cut, and count as names for the code-shape heuristics, which is what the multi-pass loop was simulating. Idempotence is a tested property, not a loop.

### 10.3 Tiers and policies

| Tier | Meaning | Examples |
| --- | --- | --- |
| T1 certain | proven by format or by an exact match | provider tokens with their checks, a private key block with a body, a JWT whose header decodes, a URL's password, an Authorization value, a value equal to a secret-named variable of the process environment (LG-D6) |
| T2 likely | judged from context | a secret-looking key whose value is random by character class and length; a command-line password; a random token near a trigger word |
| T3 floor-only | removed by the frozen baseline, judged a non-secret by the value classifier | `token: optional`, `token_endpoint: https://auth.example.com/oauth/token`, `private_key_path: ~/.ssh/id_ed25519_deploy`, `api_key_header: X-Internal-Api-Key` |

| Policy | Used by | T1 | T2 | T3 |
| --- | --- | --- | --- | --- |
| `AtRest` | hook to spool, ingester to ledger, text shown to a model or a person | mask | mask | per LG-D2: kept and counted, or masked |
| `Digest` | failure digests | mask | mask | mask, and a line that is only `NAME=value` is dropped whole |
| `Identifier` | branch names, display names | mask by format only; a format that reads as a name is kept | none | none |
| `Gate` | the store's write check and commit scan, on the whole page or hunk with its context | refuse | mask and proceed | per LG-D2 |

`Gate` is the same engine with full context and identifier exemptions as a span kind inside it, so the last check before a push is a superset of capture and no longer a weaker rerun (redact-m1). A T2 finding at the gate is rewritten and the write goes on; only T1 refuses. That removes the refused system-page line and the file left out of a commit until a person edits it (redact-7).

### 10.4 Deadline

`Scan` checks its context between lines and returns `Through`. The hook stores the redacted prefix and an honest cut (5.2). Recovery's abandoned goroutine, its three shrinking windows and its copy of the cleaning function and key pattern are deleted with the hook-sliced loop (catchup-4).

### 10.5 Typed secret references

Each T1 and T2 finding gets a handle: the first 6 hex characters of an HMAC of the value under a per-machine key in the state folder. It is stored in the event's `red` list, beside the text, not inside the marker. The marker grammar `[REDACTED:<kind>]` does not change (redact must-keep 5), so idempotence and the frozen baseline are unaffected whatever the owner decides. A consumer may render `[REDACTED:secret_value#a1b2c3]` to a model so that two mentions of one secret can be told from two secrets (redact-8); handles are meaningful on one machine only. The key name is kept by every rule, so "where the secret lives" stays in the text. Derive should drop a statement whose only content was a marker (contract C-D5).

### 10.6 The frozen baseline as an oracle (owner decision LG-D2)

Today `Redact` removes the union of the baseline's removals and the current rules', at run time. The proposal keeps `internal/redact/baseline` byte for byte and moves it from run time into the tests:

- **The property.** For every labelled true secret in the corpus, and for every secret a generator plants in a carrier text (provider formats, random tokens by class and length, passwords in the shapes the tests list), if the baseline's output does not contain it, the engine's output must not contain it. `oracle_violations` must be 0, in CI, on every change to the rules.
- **The releases.** T3 is a short, enumerated list of value classes the classifier may keep (dictionary words and their enumerations, URLs without a user part, file paths, header names, identifiers made of words). Each class has negative tests: a true secret dressed as that class must still be found.

Two things the verifier established shape this. First, amending the rule restores nothing by itself: the current rules alone also mask every listed case, so the value classifier is the real work. Second, the baseline's private-key pattern runs to the end of the text when there is no END line, so one quoted header deletes the rest of an entry (redact-9); a property over labelled secrets lets the engine bound a key block by its body, a property over "every removal the baseline ever made" would not.

**If the owner says no:** `Scan` unions its own findings with the baseline's removals, recovered by the alignment code that exists today, and tags the baseline-only ones T3, masked under every policy. Spans, tiers at the gate, the deadline, handles, repair and the corpus all still land. What stays unsolved: facts written as `key: value` in auth work stay masked for good, both engines run on every call, and the key-header truncation stays.

### 10.7 Rule changes, each gated on the corpus

| Change | Finding |
| --- | --- |
| Value classifier: random by character class and length (hex, base32, base64, digits) in place of per-string entropy at 3.5 bits | redact-1, redact-m2 |
| Structured readers: dotenv, YAML and JSON with the value on the next line, HTTP headers and cookies, connection strings, a URL's user part split at the last `@` | redact-2 |
| Wider key vocabulary and bare formats the verifier found kept whole (`*_KEY`, `signing_key`, `auth`, `credentials`, `psk`, `.netrc`, "API key" with a space, and four provider prefixes), as table entries | redact-2 |
| A private key block bounded by its body | redact-9 |
| The command table keeps its six families over one shared shell tokenizer, which the tool-record target also uses; no new families unless the corpus shows a leak | redact-10 |

### 10.8 Not in v2

A stored index of secrets seen before, so that a later bare occurrence is caught by exact match: it puts a hash lookup per token on the hot path for a case no measurement has shown. Reading `.env` and credential files to build such a set: eva-md would be opening the files it exists not to leak. Keeping legitimate zero-width joiners while removing hidden text: the cleaning rule stays as it is, and the cost to some scripts and emoji sequences stays a listed limit (redact-11). A small policy surface (extra patterns, an allowlist that can never release T1, optional personal-data classes) is a late, small package, because a team cannot add its own token format today.

---

## 11. The capture audit

Three forms of one scorer.

1. **Before the rebuild (Stage 0).** v1 gets counters for what its gate holds back (capture-m3) and a read-only command that compares a session's host transcript with its v1 inbox: turns covered, tool lines covered, duplicates, gaps by reason, share of prompts the two wrapper lists disagree on. Counts only. A week on the real install gives the rates that decide how much of sections 5 and 6 is worth building.
2. **In CI.** Recorded hook payloads and transcripts per host version, from scripted sessions with made-up content on real hosts (a developer step the owner runs or approves; it is the answer to "what do real payloads look like", which no file in the repo can give today, capture-8, catchup-6). The scorer replays them through the adapters and the ingester under generated schedules of dropped, delayed and reordered hooks, and reports the measures of section 1. `otr_leaks` and `pv_false_typed` are hard gates.
3. **On the machine.** `eva-md audit capture [--session S | --last N]` scores real sessions against their host records and prints numbers. The reconciliation counters in each session record give the same continuously, and the operations area turns them into one condition: "capture on <host> <version> is missing N% of tool calls".

During the rebuild the new path runs in shadow beside v1 (section 14), so form 3 runs on real use before anything depends on the new ledger.

---

## 12. Kept, rebuilt, removed

### 12.1 Kept as code

`internal/paths` (confinement, atomic write, `SafeName`), `internal/freetext`, `session.Lock`, the fail-closed shell of `hook.Run` and its guard list, the detached-spawn recipe, the identity resolver with its hardened git and its cache, the transcript trust checks and row classification rules of `internal/catchup` with their tests, its streaming field extractor, the redaction rules and their test corpus, `internal/redact/baseline` untouched, the removal protocol's ordering, and the test discipline (injected clocks, limits raised once, no sleep deciding an outcome).

### 12.2 Invariants, and the ones that change

Every must-keep item of the six audits holds, with these changes, each deliberate.

| Invariant today | Change | Why |
| --- | --- | --- |
| Tool lines never hold error text | A failed call may carry a bounded, strictly redacted digest | capture-1; owner decision LG-D1 |
| JSONL never rewritten; physical line numbers stable | Ids stable; one repair step may rewrite a session file atomically | capture-12, redact-3; LG-D4 |
| A record control is read in every user row, and any line that is only the phrase fires | Only typed rows are controls; On is strict | capture-10, catchup-10; LG-D7 |
| Recovered rows are the main thread's and carry no agent id | Subagent task and report are captured with their agent id, as the agent's | mcp:subagents-1; LG-D1 |
| A transcript not read to its end is treated as possibly off the record, per hook | Same meaning, enforced at release (R3), with R4 where there is no record | capture-2, catchup-2, catchup-8 |
| Off-record state in a marker and in the recovery record | In a spool control and in the session record | capture-11 |
| An entry is trusted in the file its host, session and project name | Host and session by folder and header; project is a registry attribute | capture-6 |
| The watermark is a byte offset beside the session file | Progress is a `seq` range in derive's job ledger | storage:replay-1 |
| `Redact` is the run-time union with the baseline | Per LG-D2; unchanged if the owner says no | redact-1, redact-4 |
| No retention limit | Working detail expires, if the owner agrees | LG-D3 |
| Chats outside a repo share one general area | Each folder is its own project | brief-7; LG-D8 |

### 12.3 Rebuilt

| What | Resolves |
| --- | --- |
| Event model and schema v2: ids, order, typed tool records, cut markers, provenance, taint | capture-5, capture-4, brief-10, search-7, update-model-4, update-model-11, mcp:echo-1 |
| Hook body as adapter, redact, spool append | capture-2, capture-3, capture-11, catchup-12, redact-4 |
| Ingester as the single ordered writer, with reconciliation | capture-2, catchup-1, catchup-2, catchup-3, catchup-4, catchup-7, catchup-8, catchup-9, catchup-m1, catchup-m2 |
| Capture contract additions | capture-1, capture-m1, mcp:capture-1, update-model-M3, mcp:subagents-1, mcp:provenance-1 |
| Privacy controls and lineage | capture-10, capture-m2, catchup-10 |
| Session-keyed storage, registry, catalog, one session record | capture-6, capture-11, setup-3, brief-4, brief-7, redact-12 |
| Forget, tombstones, repair, retention | capture-12, redact-3 |
| Redaction driver, tiers, gate policy, deadline | redact-3, redact-4, redact-5, redact-7, redact-m1 |
| Redaction rules under the corpus | redact-1, redact-2, redact-9, redact-m2 |
| Host adapters with version profiles | capture-9, catchup-6, catchup-11 |
| Capture audit and redaction corpus | capture-8, capture-m3, catchup-7, redact-6 |

### 12.4 Removed

- The per-hook transcript pass and the gate that reduces an event to its name while a pass is behind.
- From `internal/catchup`: the resumable scanner's saved state, the slow-row ladder, the windowed redaction and its copies of redactor internals, the grace deadlines, the durable per-row cursor, the baseline resets, the project catch-up at chat start, and the text-hash dedupe. About 1,200 of 3,070 lines by the audit's count, and about 1,400 lines of tests of that machinery.
- Six marker files, the per-session lock files' implicit state, heartbeat JSON files and their atomic saves.
- The run-time union's string alignment and its fallback, if LG-D2 is agreed; the floor rules stay, since identity fields rely on them.
- Project keys as folder names; the `general` bucket.
- The flattened tool line and every parser of it.
- Update triggers decided in the hook from file sizes (they become the worker's, from settled turns).

---

## 13. Host facts the design leans on, and how to confirm each first

All from docs and source read on 2026-11-10. The fixture recorder (LG-03) is the confirming instrument; each row is a named scenario in it. Nothing in Stage 2 is built on a row until its fixture exists.

| Fact | Used for | If it is false |
| --- | --- | --- |
| Claude Code sends `prompt_id` on every hook event | pairing events to turns (R3, R4) | pair by receipt order inside the session; R4 becomes "a prompt hook was seen since the last Stop" |
| Tool events carry a tool-use id on both hosts | pairing hook and record tool events | pair by name and input hash |
| The question tool, the plan-approval tool and the delegation tool reach PostToolUse with their input and result | `dialog`, `plan_decision`, `sub_task`, `sub_report` from the hook | take them from the record's tool rows; without a record they are lost and counted |
| SubagentStop carries `last_assistant_message` and the agent id on both hosts | `sub_report` | use the delegation tool's result |
| PostCompact carries `compact_summary` on Claude; Codex's carries only the trigger | `summary` | Codex summary from the record or not at all |
| The Claude transcript is written asynchronously and may lag | R3 is positional, not "end of file" | no change |
| Claude transcript rows carry a row id, prompt id, a version, and flags for meta, sidechain, origin and compaction summary | classification and `typed` evidence | the profile's canary fails and the session falls to the hook floor |
| A message typed while Claude is working: whether it fires UserPromptSubmit and gets its own prompt id | whether R4's hook-continuity is sound on Claude without a record | R4 stays fail-closed as written; only the count of reduced events changes |
| A fork or resume names its parent session in the payload or the record's first rows | privacy lineage | the unknown-lineage rule applies to every fork |
| Codex shows `systemMessage` to the user | visible confirmation of a control | say it in the model's context and in status, as today |
| A typed slash command reaches the hook with its name before any model call | the explicit control | the phrase remains the control; the command file only tells the model |
| Codex rollout: which history mode 0.167.0 writes, whether rows carry an ordinal, whether compressed files appear | the Codex record profile | compressed and non-local threads are `unavailable`; the hook floor applies |
| A detached child of a hook that exits in time survives on Codex (read in source, not run) and on Claude Code (v1's update jobs already rely on it) | the ingest spawn | the worker's own tick runs ingest; freshness falls to the next hook |
| Codex hook `cwd` may be a managed worktree on a detached HEAD | identity through the common git directory | none |

---

## 14. Migration: a user's inbox must survive

Nothing is deleted until the new store has been checked against the old. Four steps, each resumable and idempotent.

1. **Shadow (Stage 2).** With `capture.v2 = "shadow"`, the hook runs v1 capture unchanged, writes its output, and then appends to the v2 spool. The ingester builds the v2 ledger beside the v1 inbox. v1 stays the truth for every reader. The on-machine audit compares both against the host's records. Shadow costs no user-visible time and gives the cut-over a measured gate: v2 coverage at least v1's, zero leaks, zero misattributed prompts, over a week of real use.
2. **Cut-over.** `capture.v2 = "on"` once the consumers that read the ledger are ready. The v1 capture path leaves the hook.
3. **Import.** `ledger.MigrateV1`, under each session's lock, for every v1 session file:
   - Each physical line becomes exactly one v2 event, and for a session that v1 alone captured **`seq` equals the v1 line number**, gap lines and repaired partial lines included. So every `s:<sid8>#<line>` reference already written into a wiki resolves with no rewrite of the wiki.
   - A session that v1 wrote into two project folders (the folder changed project mid-chat) is merged by time, and its `legacy` map records, per v1 file, the line-to-seq table. A session captured in shadow keeps its v2 events and gets a table built by matching lines to events by turn, kind and text hash.
   - Kinds map one to one: prompt (`pv` typed, since v1 stored only typed prompts as prompts), reply, tool (the flattened line parsed into fields once, with the cuts v1's own updater uses, marked `legacy`), plan, host summary, events as `life`, `control` or `gap`. `rv` is 5.
   - Markers become `privacy`, `entry` and the model; the recovery record becomes `record`, with a blocked record as `privacy.off`; `.removed` becomes stub records.
   - Each v1 project key becomes a registry project with that key in `legacy_keys`: a repo key gives back its remote; a `local-<folder>-<hash>` key is matched the first time a session starts in a folder whose v1 key is that one; `general` sessions are split by the folder each entry recorded.
   - Watermarks are converted from byte offsets to `seq` and handed to derive as already-processed ranges (contract C-D3), so nothing is summarized twice.
   - The search index is dropped and rebuilt; it is a cache.
4. **Verify, then retire.** For every imported session the rendered text of each event read through the v1 backend of the reader equals the v2 one (a hash per event). Only then is `ledger/MIGRATED` written. The v1 folders are left in place for 30 days, then removed by maintenance, and the doctor says so until then. An older binary keeps working on the v1 inbox meanwhile and does not see chats captured after cut-over; the install manifest carries the minimum reader version (hosts area).

The wiki is not touched by this area's migration except through the project identity record, which the store area writes.

---

## 15. Alternatives weighed

| Question | Options | Choice and reason |
| --- | --- | --- |
| Source of the record | (a) richer hooks only; (b) transcript only; (c) hooks as floor, record as enrichment; (d) a Claude mod's `session.append` | (c). (a) is the smallest and gets failure digests, dialog answers, subagent reports and summaries, but no mid-turn text, no recovery of a missed hook, no control scan and arrival order. (b) rests on formats both hosts call unstable, and Codex may name none. (d) is one host, six weeks old, switchable off remotely; it can be a second producer of spool events later. (c) is v1's shape with the two paths given one writer |
| Privacy | (a) the per-hook gate; (b) hold and release; (c) store at once, retract on a late control, with every reader stopping at a verified point | (b). (a) drops tool lines and summaries for good and withholds the brief. (c) is the same guarantee as (b) but puts the rule in five readers |
| Store | (a) files per session; (b) an embedded transactional store; (c) v1's project folders with an alias table over them | (a). (b) breaks the two-dependency rule for no need at this scale. (c) is the smaller change and does fix the rename case, but leaves state in eight places and a session split across project folders; since schema v2 needs new files anyway, session-keyed is the simpler end state. (c) is what the migration uses |
| Project id | (a) v1 keys plus aliases; (b) minted id plus evidence; (c) the root commit as the id | (b). (c) fails on shallow clones, unborn repos, forks and merged histories |
| Redaction | (a) more rule patches; (b) a new engine from scratch; (c) rebuild the driver, keep the rules, change them under a corpus; (d) an outside scanner | (c). (a) cannot fix cost or contract. (b) discards knowledge the tests only partly pin. (d) is a dependency |
| Codex reader | (a) rollout files through a versioned profile; (b) the app-server thread API | (a) for v2. (b) is the documented reader, but it means starting the host's own process from a background job, with its cost, login and side effects unmeasured. Ingestion runs seconds after a turn, long before the 7-day compression. Revisit (b) if the audit shows Codex enrichment below the bar |
| Transition | (a) port the five v1 readers to a new API first; (b) write a v1-shaped copy for old readers; (c) shadow, then cut over | (c). (a) ports code three other areas are rebuilding. (b) keeps two stores that forgetting must both reach |

---

## 16. Contracts with the other areas

| Id | With | Direction | One line |
| --- | --- | --- | --- |
| C-D1 | derive | need | The worker runs `ingest` jobs at top priority, starting within a second of the signal, single-flight per session, with dead letters replayed after an upgrade; until it exists hooks spawn `eva-md ingest` |
| C-D2 | derive | promise | The ingester reports each settled turn and each ended session as (session, seq range); derive's triggers come from these, not from file sizes |
| C-D3 | derive | need | Derive records what it processed as (session, seq range) in its own job ledger, and accepts v1 watermarks converted to seq |
| C-D4 | derive | promise | `ledger.Episodes(session, from)` yields released events grouped by turn, with context, provenance, taint and cut markers; the text a model is shown for an event is `ledger.Render(event)`, deterministic, and `ledger.Verify(id, quote)` checks a verbatim span against it |
| C-D5 | derive | need | Only `pv` decides whose words a text is: `typed` supports stated, `chosen` supports accepted, `unknown` and `pasted` support neither without the user's confirmation; a statement whose only content is a marker is dropped; segments are not read until an evaluation says they help |
| C-S1 | store | need | The wiki holds a project identity record (id, display, remotes, root commits, legacy keys, slug, no local paths) that the registry exports and is seeded from |
| C-S2 | store | promise | `ledger.Excerpt(id, span)` returns gate-clean text with the event id and a hash, for evidence that travels; the ledger itself is never committed or synced |
| C-S3 | store | need | `ItemsCiting(event ids)` and `Retract(items, reason)`, so a forget reaches derived items and copied excerpts |
| C-S4 | store | promise | `redact.Scan(text, Gate)` on a whole page or hunk, and `redact.Version`; the store rescans every page when the version rises |
| C-L1 | deliver | promise | `ledger.Catalog(project)`, `ledger.Manifest(session)` and `ledger.Tail(session, turns)` serve a chat start without scanning raw files; privacy state is readable without a lock |
| C-L2 | deliver | need | Every injection is recorded with `ledger.AppendDelivery(session, surface, items, bytes)`: ids and hashes, never text |
| C-L3 | deliver | promise | `ledger.Walk` yields released events only, with (session, seq, gen); tool records marked `self` are eva-md's own lookups and stay out of results |
| C-L4 | deliver | need | Delivery stays silent while `privacy.state` is off, and never reads a spool |
| C-H1 | hosts | need | The plugins register the events capture needs with no tool-name filter (the binary filters): on Claude Code SessionStart, UserPromptSubmit, PostToolUse, PostToolUseFailure, SubagentStop, Stop, StopFailure, PreCompact, PostCompact, PostModelSwitch, SessionEnd; on Codex SessionStart, UserPromptSubmit, PostToolUse, SubagentStop, Stop, PreCompact, PostCompact, Interrupt, SessionEnd. New against v1: SubagentStop on both, PostCompact on Codex, and every tool. A changed declaration costs a Codex trust review, so capture's set and delivery's are declared together, once |
| C-H2 | hosts | promise | The hook entry stays `eva-md hook --host <name>` with the same stdin, stdout and fail-closed behaviour; the capture half of the adapter interface is 6.5 |
| C-H3 | hosts | need | A machine id in program-owned state; a plain statement at install of what is captured, what a model is sent and how to exclude a project; the minimum reader version in the install manifest |
| C-H4 | hosts | promise | Hook health lines (5.3) and per-session audit counters, content-free, for the freshness ledger |
| C-E1 | evaluate | need | Recorded fixtures per host version from the recorder (LG-03), and CI gates wired to the scorer's hard gates |
| C-E2 | evaluate | promise | The scorer and its measures (section 1) as a package the harness calls; the labelled redaction corpus and its rates |

---

## 17. Where the lead's draft is wrong or over-built

1. **"Readers that could leak stop at the verified point."** Do not put the rule in the readers. Keep held events in the spool and released events in the ledger; then every reader is safe by reading the only file it is given. One rule in one writer, in place of one rule in five readers.
2. **`external` as a provenance value.** Authorship and exposure are two things. A reply written after a web fetch is the agent's, and it is tainted. Make provenance the author and add a separate `taint` set computed by code from the turn's tool records, with `memory` as one of its values, which is what tells restated memory from new evidence.
3. **The provenance list lacks `chosen`.** A dialog answer or a plan approval is the user endorsing text they did not write. Filing it as typed overstates it; filing it as the agent's loses the decision.
4. **"Hooks only enqueue."** A hook must also evaluate a typed privacy control and redact before it writes. Both stay in the hook; neither can move to a job.
5. **The thread API as a reader in v2.** Keep the Codex rollout profile and treat what it cannot read as unavailable. Starting the host's own server process from a background job is not small, and nothing measured asks for it yet.
6. **A machine id on every event.** It belongs on the session header once.
7. **Typed secret references inside the marker.** Keep the marker grammar frozen and carry the handle beside the text. It costs nothing and does not depend on the baseline decision.
8. **An embedded transactional store** (raised by the capture audit, not the draft): not needed. Files, a lock and an atomic rename are enough, and they are already written and tested.
9. **Do not build the whole of this before the rates exist.** The capture verifier scoped five findings down for lack of a rate. LG-01 and LG-02 come first; if a week of real use shows the gate almost never holds an event back, the ingester is still right for structure and for the missed decisions, but its priority against the derive rebuild changes.

---

## 18. Owner decisions

| Id | Question | Options | Recommendation | If no |
| --- | --- | --- | --- | --- |
| LG-D1 | May eva-md keep more of a chat locally: bounded failure digests, subagent tasks and reports, and assistant text between tool calls? (Changes decision 16, "tool activity without output". Lead's D-E.) | full; decisions only; as v1 | Full, local only, bounded, strictest redaction on digests, with LG-D3's expiry. Dialog answers and plan decisions are kept in every option | `capture.detail = "decisions"`: typed tool records with exit codes only. Cost: error-to-fix lessons and subagent findings reach memory only when a final reply restates them |
| LG-D2 | May the frozen baseline move from a run-time union to a test oracle over true secrets, with an enumerated list of released non-secret classes? (Amends the AGENTS.md rule; the package itself stays untouched.) | oracle; union as today | Oracle | Section 10.6's fallback: union kept by alignment. Cost: `key: value` facts in auth work stay masked for good, double CPU on every call, the key-header truncation stays |
| LG-D3 | May working detail (segments, failure digests, subagent reports) expire after 180 days? (v1 rules out automatic deletion.) | expire detail; keep everything | Expire detail; the conversation itself keeps no limit | Keep everything; the doctor reports size. Cost: the riskiest class of text is kept for good |
| LG-D4 | May a session file be rewritten by one maintenance step that keeps every id? (v1: never rewritten.) | yes; no | Yes | Forgetting stays whole-session; a secret a rule missed stays on disk until its whole chat is removed; better rules protect only what is read, not what is stored |
| LG-D5 | May eva-md read into the ledger, with no model call, the chats of a project from the 14 days before its first hook there? (v1 rules out history backfill. Lead's D-M.) | on, stated once at install; opt-in; off | Opt-in for v2.0, on by default once the audit has run on real hosts. The ordered scan honours any control typed at the time | Off. Cost: the first chat that remembers is the second |
| LG-D6 | May the hook compare text against the values of secret-named variables in its own process environment, in memory only? | yes; no | Yes: no file is read, nothing is stored, no false positives | Rules only |
| LG-D7 | Tighten the phrases: only a typed prompt is a control, and "back on the record" must be the whole prompt? (Refines decision 16's off switch.) | tighten; as v1 | Tighten, with confirmation shown on both hosts and an explicit command | As v1. Cost: a pasted line can turn capture back on |
| LG-D8 | Give each project an identity record with aliases, and each folder outside git its own project? (Refines decision 10.) | yes; aliases only; as v1 | Yes | Aliases only: renames and first pushes are fixed; folders outside git still share one area, with the folder recorded so delivery can filter |

---

## 19. Risks

1. **Host formats move weekly.** Enrichment (segments, order, recovered turns) can degrade on any release. The floor, the canary, version profiles and fixtures make that a loss of detail, not of correctness; the audit must show it within a day.
2. **Held events and a dead ingester.** If the job never runs, memory stalls with nothing lost and nothing visible. R7, the dirty sweep and one operations condition cover it; the acceptance test kills the ingester and checks the condition appears.
3. **More text at rest.** Failure digests and subagent reports are where secrets live. Bounds, the strictest policy, expiry, repair and forget-by-value are the answer; the residual is that redaction misses some passwords, as today.
4. **R4 costs content** on sessions with no readable record when a prompt hook was missed. Counted; if the rate is material on Codex, the thread API reader is the remedy.
5. **A wrong automatic alias** would merge two projects' memory. Aliasing needs two kinds of evidence, never merges on root commit alone, and every alias is a reversible registry edit.
6. **The driver rebuild could weaken redaction silently.** The differential test against v1's current rules, the oracle, the corpus rates and a counts-only shadow run on the user's own ledger come before any rule changes meaning.
7. **Migration.** The line-to-seq identity is exact only for sessions v1 alone captured; the table covers the rest, and step 4's per-event hash check gates retirement.
8. **Allowlist too strict.** A new host surface stores prompts as `unknown` and promotes nothing from them until a release adds it. `unknown_pv` by entrypoint is an audit number so this is seen.
9. **This area is on every other area's path.** The reader API and the event model are specified first (LG-10) so the other tracks build against a contract, not against this code.

---

## 20. Work packages

Stages: 0 ground truth, 1 foundations, 2 core rebuilds, 3 memory quality, 5 proof. Sizes: S up to two days of one agent, M up to a week, L more.

| Id | Stage | Size | Outcome | Scope | Depends on | Acceptance, and the measure that must move |
| --- | --- | --- | --- | --- | --- | --- |
| LG-01 | 0 | S | v1 says how often its gate holds an event back and what that cost | `internal/hook` heartbeat record, one doctor row. No behaviour change | none | A test holds an event behind recovery and reads `held: prompt`, `reply`, `tool` or `brief` in the run record; the doctor row shows 7-day counts. Measure: a held-back rate exists |
| LG-02 | 0 | M | A read-only audit of v1 capture against host transcripts | new `eva-md audit capture`, `internal/ledger/audit` scorer on v1 files. Out of scope: any fix | none | On a synthetic transcript and inbox with planted losses it reports the exact counts; a test asserts its output holds no fixture text. Measure: `turn_coverage`, `tool_coverage`, `dupes`, gap rate for v1 exist |
| LG-03 | 0 | M | Recorded payloads and transcripts for Claude Code 2.1.321 and Codex 0.167.0 | a recorder hook and scrubber for developers, `testdata/<host>/<version>/` with the scenarios of section 13. Needs a person to run it on real hosts | none | Each row of section 13 has a fixture and a recorded answer; fixtures hold only made-up values (a test scans them with the redactor and for the recorder's user name) |
| LG-10 | 1 | M | The event model as a spec and a package | `internal/ledger/event`: types, codec, validation, ids and refs, provenance and taint enums; `ledger.Reader` interface with a read-only v1 backend. A short spec and decision records | none | Golden files round-trip; a v1 inbox fixture read through the v1 backend yields events with seq equal to line; `go vet` and both test runs pass |
| LG-11 | 1 | M | Redaction driver with spans, same behaviour | `internal/redact`: `Scan`, `Apply`, tiers as labels only, context deadline; `Redact` as a wrapper. Out of scope: any rule change; `baseline` untouched | none | Differential test: `Apply(Scan(x))` equals v1 `Redact(x)` on every existing test case and on 150,000 generated inputs; idempotence holds; benchmark: at least twice as fast at 16 KiB on reply-like text, linear to 256 KiB; a cancelled scan returns a prefix |
| LG-12 | 1 | M | A labelled corpus, rates and the oracle in CI | `internal/redact/testdata`: positives by format and shape, negatives from code, docs and chat lines, generators; `TestOracle`; `eva-md audit redaction` (counts by kind and tier over the local store) | LG-11 | CI prints recall by class, `fact_loss_rate`, `hex32_kept_rate`; `oracle_violations` is 0 for the current rules; the audit command prints no content |
| LG-13 | 1 | M | Hook payloads decode through adapters with provenance by allowlist | `internal/hostadapter` capture half for both hosts, one wrapper list. Out of scope: storage | LG-03, LG-10 | Every fixture payload decodes to the labelled events; `pv_false_typed` is 0 on the fixtures; an unknown entrypoint yields `unknown` with its text |
| LG-20 | 2 | L | The hook writes the spool and nothing else | `internal/ledger/store` (layout, session record, spool append, health lines), `internal/hook` capture body, config key `capture.v2`; shadow mode after v1's output | LG-10, LG-11, LG-13 | Hook tests for every event and guard pass against the spool; benchmark keys `capture_16k_p95_ms` under 50, `capture_64k_p95_ms` under 120, `tool_event_p95_ms` under 25; in shadow, v1's benchmark figures do not move |
| LG-21 | 2 | L | The ingester releases an ordered ledger | `internal/ledger/ingest`: record readers per host (rules ported from `internal/catchup` with their tests), reconciliation, R1 to R7, idempotent append, manifest, catalog, audit counters, `eva-md ingest`. Deletes the hook-sliced recovery | LG-20 | Scorer on fixtures under generated hook-drop schedules: `otr_leaks` 0, `dupes` 0, `order_agreement` at least 99.9%, `turn_coverage` at least 99.5%, `tool_coverage` at least 99%; killing the ingester between any two steps and rerunning yields the same ledger |
| LG-22 | 2 | M | Projects keep their identity | `internal/ledger/registry`, resolver changes in `internal/identity` (common directory, host forms), `eva-md project list|merge|alias` | LG-10 | Scripted scenarios (first push, renamed remote, moved folder, linked worktree, detached worktree, two folders outside git, fork) give `identity_continuity` 100% and never merge the fork |
| LG-23 | 2 | M | Decisions and delegated work are in the record | adapters and ingester: typed tool records, `dialog`, `plan_decision`, `tasks`, `sub_task`, `sub_report`, `summary` on Codex, `delivery` API; failure digests and segments behind `capture.detail` | LG-21, LG-D1 | `decision_capture` 100% on fixtures; a failed command's digest is within bounds and passes the `Digest` policy; a successful command stores no output (test asserts) |
| LG-24 | 2 | M | Privacy controls that can be seen and trusted | control rules, explicit command, `eva-md record`, lineage, late-control notice | LG-21, LG-D7 | The v1 control tests pass with the changed cases flipped; a pasted "back on the record" does not resume; a fork of an off chat stores no summary; a late Off deletes held events and is announced once |
| LG-25 | 2 | M | The v1 inbox becomes the ledger with every reference intact | `ledger.MigrateV1`, verification, retirement rule | LG-21, LG-22 | On a v1 fixture inbox with two-project sessions, removed sessions and off-record ranges: every `s:` reference resolves to the same text; per-event hashes match; a second run changes nothing; v1 files are untouched until the mark is written |
| LG-26 | 2 | M | Redaction that keeps facts and finds more secrets | value classifier, structured readers, wider vocabulary, key-block bound, tiers with meaning, `Gate` and `Digest` policies, the oracle switch | LG-12, LG-D2 | `oracle_violations` 0; `fact_loss_rate` at most a fifth of v1's (with LG-D2) or unchanged (without); `hex32_kept_rate` under 1%; every shape the verifier found kept whole is now found; the store's gate flags a value on the line after its key |
| LG-30 | 3 | M | Forgetting one line, and repair | `eva-md forget`, tombstones, repair generations, expiry, the cascade notice | LG-21, LG-D3, LG-D4, C-S3 | After a forget, a byte search of the ledger folder and the index finds none of the text (`forget_residue_bytes` 0); refs to the forgotten events resolve to a stub; a rule version bump followed by repair masks a planted secret in stored events |
| LG-31 | 3 | S | Typed secret handles; exact match on the process environment | `red` handles, per-machine key, in-memory environment set | LG-26, LG-D6 | Two mentions of one made-up secret share a handle and two secrets do not; the marker grammar test is unchanged; a secret-named variable's value printed in a digest is masked |
| LG-32 | 3 | S | A small redaction policy surface | config: extra patterns, allowlist that cannot release T1, optional personal-data classes | LG-26 | A custom pattern masks; an allowlisted T2 value is kept and a T1 value is not |
| LG-33 | 3 | M | Bounded backfill into the ledger, no model | `eva-md ingest --backfill` from offset 0 for one project, 14 days | LG-21, LG-D5 | On fixtures, a control typed in an old transcript is honoured; nothing older than the bound is read; no model or network call (test seam) |
| LG-50 | 5 | M | The audit as a gate, and a week of proof | scorer in CI as hard gates; `eva-md audit capture` on v2; a week of shadow on the real install | LG-21, LG-23, LG-24 | CI fails on a seeded leak and a seeded misattribution; the real install's week meets section 1's starred bars, or the plan records which did not and why |

Order that matters: LG-01 to LG-03 before the scope of LG-20 and LG-21 is fixed; LG-10 before other areas start on the reader; LG-11 before any rule change; shadow before cut-over; LG-25 only at cut-over.

---

## 21. What a field report from a real install should answer for this area

In general terms, counts only, nothing quoted.

1. How many inbox entries of each kind and origin (hook or recovery) per host; what share are gap events, by reason; what share are host prompts.
2. For a sample of recent chats: how many turns and tool calls the host's own transcript holds against how many the inbox holds; any duplicates.
3. Hook run times by event (median and p95) and failed runs by event, per host.
4. Sizes: sessions, total bytes, the largest session, the median reply size, the share of entries that were cut, and how many redactions per thousand entries.
5. What the host transcripts look like: version values seen, whether rows carry ids and turn ids, the share of assistant rows that are mid-turn text, on Codex which history mode and whether compressed or missing transcripts occur, and how often a hook names no transcript.
6. How often the host's question, plan-approval and delegation tools appear in transcripts.
7. Project identity: how many project keys, how many are local keys, any two that look like one project, and how many distinct folders sit in the general area.
8. Off the record: how many controls were recorded, and whether any chat is off without the user having asked.
9. Redaction markers by kind in the inbox and in wiki pages, and the share that sit as the value of a `key: value` line.
10. Which entrypoint values and session-start sources (startup, resume, fork, clear, compact) occur.
