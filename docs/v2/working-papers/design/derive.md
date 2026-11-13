# Organizing: the worker, the extraction contract and consolidation (L1 Worker, L2 Derivation, the semantics of L3)

Design section for the eva-md v2 plan. Area: the control plane (one worker, a work ledger, priorities, dead letters, a circuit breaker, gates, hooks as wake-ups, the scheduled tick and its fallback), the input model (turn-structured episodes and a real cursor), extraction contract v2, the tiers and what each writes, the semantics of the memory model (kinds, tags, lifecycle, supersede rules, branch-provisional knowledge, workstreams), what happens to system pages, the model runner and its policy, and cost accounting.

Finding ids are the register's (`docs/v2/findings.md`): `UPM-n` (update, first half), `UPW-n` (update, second half), `PGS-n` (memory model), `SYS-n` (system pages), `PRD-n`, `STO-n`, `OPS-n`, `EVL-n`, `MCP-n`, `RCL-n`. Every audit's verification section was read, including the catch-up and brief ones, which were complete when this was written. No finding this design rests on was refuted. Where a second reader scoped a finding down, the design follows the second reader: UPW-01 is "lost from pages, not from the record" (the raw chat is still in the inbox), UPM-09 is an evolution of an existing seam and not a rebuild, UPM-15 is low and rides with the input rebuild, PGS-02 is a prediction until a live run exists, and SYS-01 to SYS-14 are mostly medium or low.

Two sibling designs existed when this was written and are taken as given: the ledger design (events with `seq`, `pv`, `taint`; `ledger.Episodes`, `ledger.Render`, `ledger.Verify`, `ledger.Excerpt`; contracts C-D1 to C-D5) and the store design (`store.Apply(Txn)`, `store.Plan`, `store.Coverage`, items and episodes as files, per-operation outcomes; contracts S-D1 to S-D7). Where this design differs from either, section 14 says so in one line.

Everything here is a proposal. Numbers given as bars are targets. Most have no baseline yet, and producing that baseline is the first three work packages.

---

## 1. Goal and the measurable bar

**Goal.** Every settled part of every chat is organized exactly once into small, cited, typed records, soon enough that the next chat starts from them, at a cost the user does not notice. Nothing is ever passed over without a record of why and a way to try again. Who said a thing is decided by the record, never by resemblance between two texts. What the user stated is never changed by anything but the user. When the host changes under eva-md, organizing pauses itself, burns almost nothing, loses nothing, and resumes by itself.

**The bar.** "Level 0" is CI with recorded or canned model answers. "Level 1" is a live run on a real host, which needs the owner's leave each time because it spends model quota.

| Measure | Bar | Today |
| --- | --- | --- |
| `ranges_unaccounted`: released ledger ranges that are neither covered `done`, covered `skipped` with a reason, covered `refused` with a reason, nor pending | 0 (hard gate, Level 0, aging suite) | the watermark moves past refused, skipped and quarantined content with no record of the range (UPW-01) |
| `lost_wakes`: sessions left unorganized by a wake that found a job running | 0 (hard gate, Level 0) | a busy lock drops the trigger (UPW-04) |
| `end_to_saved`: from a chat's end or compaction to its memory on file, when the gates allow | p50 at most 90 s, p90 at most 5 min (Level 1) | minutes to the next day; never measured (EVL-07) |
| The same for a chat that never ends | at most the tick interval plus 5 min, with the tick; next activity without it | until 60 KB, a 30-minute gap or another chat's start |
| `fact_recall`: planted facts that reach the store with the right value (value-exact, black-box, Level 1) | at least 0.90 at the default model and effort; at least 0.95 after one escalation | never measured live (UPM-01, EVL-02) |
| `wrong_write`: stored statements no cited span supports | at most 0.02 | not measured (EVL-03 scores by codename) |
| `misattributed`: text of the agent, a tool, a paste or an outside source stored under the user's tag | 0 (hard gate, Level 0 and 1) | 0 on canned tests only |
| `honest_refused`: user-tagged statements a faithful model wrote that the checker refused | at most 5% | most natural phrasing, by construction (UPW-03); unmeasured |
| `accepted_captured`: decisions the user made by agreeing to a proposal, by a dialog answer or by a plan approval, that become durable decisions | at least 80% of fixtures | 0% as knowledge (PGS-02, UPM-08) |
| `stale_in_force`: superseded statements still active | at most 0.02 | not measured |
| `stored_with_authority`: payloads of the poisoning corpus stored under the user's tag or as a standing rule | 0 (hard gate) | not measured (EVL-12) |
| Language parity: acceptance on translated twins of the fact scripts (one spaced language, one unspaced) | within 5 points of English | fails for unspaced scripts (UPM-11, PGS-12) |
| Host-break containment: model calls burned after a host change that breaks output | at most 3, then 1 probe per backoff step; 0 sessions blamed; 0 ranges lost | 3 per session per day, then the chunk is skipped (UPM-03) |
| Whole-answer rejections | only for an answer that is not the schema's JSON | also for bookkeeping: half the ids, three dates (UPW-07) |
| Background share of a day's tokens | inside the budget; no background call for older work while a turn of that host is open | 40 calls a host a day at the highest effort (UPM-05) |
| Tokens per organized chat | at most a fifth of v1's measured figure (set after DV-02 and DV-03) | unknown (EVL-08) |

---

## 2. The design in one page

```
 hook (capture, then one detached spawn)          OS tick, every 15 min (owner decision DV-D1)
        │  eva-md work --wake                              │  eva-md work --tick
        ▼                                                  ▼
 ┌──────────────────────────── one worker at a time (worker.lock, one waiter) ───────────────────────────┐
 │ ingest (ledger's job, no model)  ─►  plan: ledger released ranges − store.Coverage = pending           │
 │ ripeness rules ─► priorities ─► gates (budget, quota, idle) ─► breaker                                 │
 │                                                                                                         │
 │ fast pass, per settled episode:                                                                         │
 │   episode builder ─► model runner (small model, low effort, sealed) ─► checker ─► one repair round     │
 │        ▲ ledger.Episodes, Render          typed outcome                │ ledger.Verify (exact spans)   │
 │        │                                                               ▼                               │
 │   related items (deliver's retrieval core)            Txn{pass, ops, covers} ─► store.Apply ─► 1 commit│
 │                                                                                                         │
 │ housekeeping, per tick, no model: lifecycle, fold merged work, exact duplicates, support, drift labels  │
 │ review, gated, stronger model: near-duplicates and disputes, through store.Plan as the lint gate        │
 └────────────── work journal (content-free): attempts, outcomes, usage, breaker, budget ─────────────────┘
```

Eight rules carry the design.

1. **Pending work is computed, never queued.** What is pending is what the ledger has released minus what the store's coverage holds. Wake marks and the journal are hints and a cache. A lost hint costs time, never work.
2. **One worker, one waiter, no lost wake.** Hooks never wait and never take a job's lock. A wake that finds the worker busy runs the ledger's ingest itself, then becomes the single waiter or exits because a waiter exists.
3. **A range ends in exactly one of three recorded states**: done, skipped with a reason, or refused with a reason and the contract version. Refused ranges are dead letters and are replayed when something that could change the answer changes.
4. **Blame the content only on positive evidence.** An unrecognized failure is the host's until a control run succeeds on the same host and the same range fails again.
5. **The model proposes small operations with the exact words they rest on.** Code checks each quote by exact match against the rendered event, takes the tag from the event's own provenance and taint, and applies each operation on its own. Silence means keep. No model writes a page, an id, a date or a tag that code does not check.
6. **The user's tier is closed.** A statement under the user's tag changes only on the user's later words. A contradicting observation is recorded beside it as a dispute.
7. **Most of consolidation is code.** Lifecycle, folding merged work, exact duplicates, support counting and drift labels need no model. A model review of near-duplicates and disputes is built only if the corpus audit shows a need, and it goes through the same checker and the store's plan step.
8. **Model, effort and budget are a measured policy.** A small model at low effort by default, chosen from a cost and fidelity frontier measured on real hosts, with one step of escalation and a token budget.

What is deliberately not here: no resident process, no embedded database, no new dependency, no model or network call in a hook, no model-written summary or overview, no topic clustering, no graph, no second copy of chat text outside the ledger and the bounded excerpts the store keeps.

---

## 3. The control plane

### 3.1 Processes and locks

- `eva-md work --wake --reason <event> --session <host>:<id>`: started by a hook after its output is written, with v1's detached-spawn recipe (own session, no inherited pipes, the installed binary by absolute path, an allowlisted environment). The hook does nothing else for this area.
- `eva-md work --tick`: started by the OS scheduler. Same code path with no session named.
- `eva-md work --session <host>:<id> [--dry-run] [--foreground]`: a person's run. It overrides the gates as v1's manual run does and, when it succeeds, clears holds (update-write must-keep 12). `eva-md update` stays as an alias for one release.
- `eva-md work --status`: counts only: pending ranges and their oldest age, refused ranges by reason, breaker and hold states, today's usage against the budget.

Lock protocol (all locks are `paths.TryLock` flocks in the locks folder, so a dead process leaves nothing behind):

1. A wake first runs `ledger.Ingest` for its session. Ingest needs only the session lock, so it never waits for a model call (ledger contract C-D1).
2. It leaves a wake mark, `work/wake/<host>-<session>`, an empty file.
3. It tries `worker.lock`. If it gets it, it is the worker.
4. If the worker lock is busy, it tries `waiter.lock` without waiting. If that is busy too, it exits: a waiter already exists and will see the mark.
5. As the waiter it blocks on `worker.lock`, takes it when the worker releases it, drops `waiter.lock` and becomes the worker.

At most two processes of this kind exist at any time, however many hooks fire. The worker consumes marks at the top of each loop. A mark written while the worker is finishing is seen by the waiter, or, when there is no waiter, by the worker's last check after it lists pending work again. Tests of this handoff block each side on a channel the test releases; no sleep decides an outcome (the project's test rule).

### 3.2 The worker loop

```
recover: a journal "start" with no "finish" is an interrupted attempt; count its call in the budget
loop:
    consume wake marks; ingest any session the ledger marks dirty
    reconcile to store.Coverage when the store's generation moved without this worker (restore, merge, undo --redo)
    pending   = ledger released ranges − coverage           (computed, per session)
    ripe      = ripeness rules over pending                 (3.4)
    job       = highest priority ripe job the gates and the breaker allow   (3.5 to 3.7)
    if none and no timed step is due: break
    journal start (fsync) → run → journal finish (fsync)
unlock; list once more; exit
```

The worker runs one model call at a time. Calls for two hosts could run side by side, since their quotas are separate, but the second reader of UPM-05 is right that parallel passes add to rate-limit competition; serial is the v2.0 choice and `end_to_saved` decides whether to revisit it.

### 3.3 The work ledger

Under `~/.local/state/eva-md/work/`, folders 0700, files 0600, opened through `paths.OpenRoot`.

| File | Content | Durability |
| --- | --- | --- |
| `journal/<YYYY-MM>.jsonl` | one line per job transition; counts, ids, enums, never text | append under flock, fsync per line; kept 400 days |
| `state.json` | a snapshot: per-session cursor cache, breakers, holds, budget counters, probe results, tick times | atomic replace; rebuilt from the journal and `store.Coverage` when missing or unreadable |
| `wake/<host>-<session>` | empty marks | best effort |
| `pending/<pass>.json` | a validated operation batch waiting for `store.Apply` | the one file here that holds memory text; removed when the commit returns; removed by a forget of its session |

A journal line:

```json
{"t":"2026-11-10T07:01:02.123Z","pass":"01JD3K8Q2M7V5T9R4N6W0X1Y2Z","ev":"start|finish|interrupted",
 "kind":"extract|house|review|replay|write|probe","prio":2,
 "session":"claude-code:5f0c9a1e-…","project":"p_k3v9x2ab7q1m","ws":"w01k2m3n4p5q6a","range":[120,184],
 "contract":5,"attempt":1,
 "runner":{"host":"claude-code","backend":"cli","billing":"plan","cli":"2.1.321",
           "model_asked":"haiku","model_answered":"claude-haiku-…","effort":"low"},
 "class":"ok|auth|rate_limit|host|timeout|answer_invalid|tool_used|cli_missing|store",
 "outcome":"applied|none|refusals|refused|skipped|deferred","reason":"",
 "ops":{"proposed":9,"applied":7,"mended":1,"refused":2,"repaired":1,"by_code":{"quote_not_found":1,"tag_too_low":1}},
 "usage":{"in":8123,"cached_in":0,"out":611,"calls":2,"ms":14200},
 "commit":"3fa9c1…","settle_to_saved_ms":48211}
```

The store's coverage is the truth about progress (store contract S-D3). The journal is the truth about attempts, cost and host health, which the store does not hold. Nothing in v1's five places of job state survives: the byte watermark, the two-day run-log scan, the sweep file and the baseline record are replaced by coverage plus this journal (UPW-09).

### 3.4 Job kinds, priorities and ripeness

| Priority | Job | Model | When |
| --- | --- | --- | --- |
| P0 | `ingest` (the ledger's) | no | every wake, outside the worker lock |
| P1 | `write`: remember, correct, confirm, forget from the person or the agent's tool | no | synchronous in the calling process, through the same checker and `store.Apply`; journaled; never queued behind a model call |
| P2 | `extract` for a session that just ended or compacted | yes | at once |
| P3 | `extract` for other ripe sessions | yes | current project first, then round-robin by project, newest session first |
| P4 | `house` and the other areas' timed steps (`store.Sync`, `store.Maintain`, the index refresh, health, the upgrade check) | no | per tick, when due |
| P5 | `review` | yes | at most once a day a project, when housekeeping left judgement work, idle and quota allowing |
| P6 | `replay` of refused ranges, and an opted-in backfill | yes | at most 30% of the day's budget, newest first |

Other areas register a job as `{kind, priority, due(), run()}`. This area owns the runner, the journal and the order, not their bodies.

**Ripeness** replaces v1's six triggers and the day's sweep. It is decided by the worker from the ledger's session records, never by which hook fired.

| Rule | A session's pending settled turns are ripe when | Replaces |
| --- | --- | --- |
| ended | the ledger holds an end or a compaction for it | SessionEnd and PreCompact triggers |
| idle | its last event is at least 5 minutes old | the 2-minute chat-start catch-up; the day's sweep |
| long | at least 8 settled turns or 24 KB of rendered text are pending | the 60 KB Stop trigger |
| aged | it has been idle 6 hours, whatever the size | the short chat with no end that waits for ever (update-write 3.4) |
| asked | a person named it | manual runs |

A turn that is still open is never ripe. A range with no typed prompt is covered as `skipped: no_user_turn`. A program-run session is covered as `skipped: program` with no call (update-write must-keep 13). An ended session whose pending text is under 600 characters with no typed prompt over 40 characters is covered as `skipped: small`. Every skip is a coverage row, so it can be counted and replayed.

**Fairness** (UPW-13). P2 always goes first. Within P3 the worker takes one episode a project in turn, so one long idle session cannot hold the day. P5 and P6 together may use at most 30% of a day's budget, and 15% is held back for P2 until local midnight.

### 3.5 Gates

| Gate | Rule | Source of the signal |
| --- | --- | --- |
| Budget | per host and day, in weighted tokens: input, plus cached input at a tenth, plus output at five times. A job whose estimated input does not fit is deferred. Default provisional until DV-03 | the journal |
| Quota | when the host reports its remaining allowance: P3 to P6 stand down under 25% remaining, P2 under 10%. After any `rate_limit` outcome: hold the host until the reset time when given, else 30 minutes doubling to 6 hours. A rate-limit failure of the user's own turn (the ledger's `life` event `stop_failure`) holds background work for that host for an hour | the runner's typed outcome; the ledger |
| Idle | P3 to P6 wait while any session of that host has an open turn or an event in the last 2 minutes. P5 and P6 need 10 quiet minutes on every host | the ledger's session records |
| Off | `organize = false` in the config stops every model job at once (v1's `updates` key, kept under both names for a release) | config |

The 25% figure is Codex's own memory writer's. Whether either host exposes its remaining allowance to a headless run is not established (section 11); the budget, the hold after a rate limit and the idle gate work without it.

### 3.6 Outcome classes, consequences and the breaker

The runner returns a typed outcome (section 8). v1's three consequences are kept (update-model must-keep 23); what changes is the default.

| Class | Meaning | Consequence |
| --- | --- | --- |
| `ok` | a completed call, tools unused, an answer that is the schema's JSON | the checker runs; the range is covered whatever it refuses |
| `auth` | the host says sign in | hold the host. One probe a day, and one when the host next shows chat activity. A condition for the hosts area: the one case in this area where a person alone can act |
| `rate_limit` | the host says wait | the quota gate's hold |
| `cli_missing` | no CLI at the recorded path or on PATH | back off; probe the path again next tick |
| `host` | everything unrecognized: an unknown event type, no envelope, an exit the classifier does not know, a refused flag | counts toward the breaker; no session is blamed; no coverage is written |
| `timeout`, `answer_invalid`, `tool_used` | ambiguous: the host or the content | count toward the breaker first; see the control rule below |
| `store` | `store.Apply` failed after a valid answer | the batch stays in `pending/`; retried without a model call |

**The control rule.** An ambiguous failure is attributed to the content only when the same range fails again after another range, or the known-good live fixture, completed `ok` on the same host, backend and model in between. Then the range is covered `refused` with the class as its reason and becomes a dead letter. Until then it stays pending. v1 does the opposite: `invalid_output` is the default class and counts against the session (UPM-03).

**Adaptation before blame** (UPM-07). A timeout is a load signal. The second attempt takes half the turns at one effort step lower. The third, on a later loop, takes one turn. An `answer_invalid` gets the repair round first (5.8), then one attempt on the escalation model.

**The breaker**, per host and backend:

```
closed ──3 non-ok outcomes of class host/timeout/answer_invalid/tool_used in a row, any sessions──► open
open ──after 15 min, doubling to 24 h; at once when the CLI version or eva-md's version changes──► half-open
half-open: one call on the known-good live fixture
    ok   ──► closed; pending ranges run again; a range that then fails alone is content
    else ──► open, next step of the backoff
```

While open, no model job runs for that host. An open breaker is an automatic condition for 24 hours and says nothing to the user; after that the hosts area may say that organizing is paused until eva-md is updated. A host release that adds one event type therefore costs three calls and a few probes, not three calls a session a day and a skipped chunk each.

### 3.7 Dead letters and replay

A dead letter is a coverage row with outcome `refused`, a reason and the contract version (store 7.5), plus per-operation refusals counted in the journal. The raw text stays in the ledger, so nothing is lost from the record (the UPW-01 verifier's correction).

| Trigger | What is replayed |
| --- | --- |
| eva-md upgraded and the contract version rose, or the release notes' replay hint names a reason code | ranges refused under an older contract; ranges covered `done` with refused operations of a code the new contract changed |
| The host CLI's version changed | ranges refused with a host-shaped reason |
| The model policy changed | ranges refused as `answer_invalid` or `timeout` |
| A person asks (`eva-md work --replay`, or the store's `undo --redo`) | the named ranges |

Replay is P6: inside its budget share, newest first, at most twice per range per runner fingerprint. A replayed range goes through the same checker, and duplicates of what the first pass stored are merged as support, never added twice (5.7, step 9).

### 3.8 The tick, and the fallback if the owner declines it

**With the tick (recommended, DV-D1).** One OS-scheduled, short-lived run: a launchd user agent with `StartInterval` 900 and `RunAtLoad`, and a systemd user timer on Linux. The hosts area installs and removes it. A tick with nothing due reads `state.json` and exits. The tick is what makes idle sessions ripe on time, runs housekeeping and the other areas' timed steps, and keeps working when hooks do not fire (OPS-01, OPS-08). It is not a resident process, but it reverses the design's "no sleeping jobs", so it needs the owner's yes.

**Without it.** Every rule above still holds; only the clock changes.

- Hook wakes carry the tick: a worker woken by any hook also runs whatever timed step is due.
- The MCP server, a process the host already keeps alive while a chat is open, calls `eva-md work --tick` every 10 minutes. That covers a chat left open and idle.
- With every chat closed nothing runs. A chat that never fired an end is organized at the next activity of any chat, and sync, health and the upgrade check wait with it. Friday's last chat is organized on Monday's first hook, as today.

### 3.9 Crash safety and exactly-once

- The journal `start` line is written and synced before the call. If it cannot be written, no call is made (update-write must-keep 3, in its new form).
- A pass id is minted when the job is created. `store.Apply` is exactly-once per pass id (store 7.1), and ids minted in a pass are a function of the pass, so a retry after a crash writes the same files.
- A validated batch is saved to `pending/<pass>.json` before `Apply`. A failure after the call, in the store's lock, a save or the commit, costs no second call and cannot get a different answer the second time (the UPW-09 verifier's addition).
- A range is covered only by the commit that holds its operations. There is no state in which the pages moved and the progress marker did not, or the reverse.
- A job killed inside a call leaves a `start` with no `finish`. The next run records it as interrupted and counts the call in the budget. v1 records nothing (update-write F9).

---

## 4. The input model: episodes and a cursor

**The unit is an episode**: consecutive settled turns of one session, cut only between turns.

| Rule | Value | Finding |
| --- | --- | --- |
| Cut | at turn boundaries only. A turn is settled when the ledger has released its reply, or its interruption or failure | UPM-04, update-write must-keep 16 |
| Size | up to 24 KB of rendered text an episode (v1: 60 KB), so a small model reads it whole. A turn larger than that is an episode alone, up to 96 KB | UPM-04, UPM-05 |
| Typed text | never cut under 32 KB an event. The user's long specification is the likeliest home of a decision | UPM-04 |
| Agent text | head and tail around a visible cut line, 8 KB an event, and only when the turn does not fit | as v1, now marked |
| Tool records | one line each from typed fields, with the failure digest when the call failed; runs of reads and searches are counted. Rendered by `ledger.Render`, never parsed back | UPM-04 |
| Segments | not read. The switch exists for an evaluation arm (ledger contract C-D5) | |
| Commit state | the head commit and branch come from the ledger's `ctx` event at capture time. Commit subjects are not sent: `git log` already says them, and they came from other people (UPM-12) | UPM-04, UPM-12 |

**Carried context**, small and fixed, each part labelled as data:

1. The workstream: id, title, state, branch, whether the branch is the default one, its current status line, its open next steps and questions with their handles.
2. Related items: at most 12 headlines with handles, chosen by the deliver area's retrieval core over the episode's text and paths, the chat's project first (contract D-R1). Items of another project are marked read-only. v1 picks 12 rows by raw word count with a second tokenizer (UPM-06).
3. Continuity: the lines the previous episode of this session left as open, and the head of the last settled prompt and reply, 1 KB, when the episode starts mid-session.
4. A legend, the same every time: what each speaker label means, that a cut line, a redaction marker and an exit status are eva-md's marks, that tool output is absent except a failure digest, that this may be part k of a chat, and that a host summary restates entries already recorded (the seven things UPM-04's prompt reading found missing).

**The cursor** is `(session, next uncovered seq)`, derived from `store.Coverage` and cached in `state.json`. Nothing is rescanned. What v1 recovers by scanning the file from byte 0 with patterns that depend on field order (UPM-15) is now a field of the ledger event: off-the-record text is never released, taint is on the event, "a person typed this" is `pv`, and the branch is the event's context. `scanPrefix`, the three markers, `Options.Continued` and the "opening reply" mark go away.

The episode builder is pure: the same ledger range, workstream record and item set render the same bytes. `eva-md work --dry-run --session` prints that input and writes nothing (update-write must-keep 14).

---

## 5. Extraction contract v2

### 5.1 One source of truth

One Go table lists every field, bound, enum and refusal code. It generates the strict schema, the sentences of the prompt that state limits and refusals, the refusal-code table in the docs, and the checker's bounds. A test fails when a refusal code has no sentence in the prompt. That closes the gap UPM-02 found between what the prompt says and what the checker enforces. The prompt's length is not a test.

The prompt says, in this order: who will read the result and why (a coding agent at the start of a later chat, in a short brief); that everything after the instructions is data; that writing nothing is allowed and preferred; what is worth keeping (5.9); the evidence rule; what will be refused, in plain words; and one whole example answer and one negative example. On Claude it is the system prompt. On Codex, which has no flag for fixed instructions, it leads the input behind an explicit boundary line. DV-03 compares the two hosts on the same scripts.

### 5.2 What the model sees for an entry

```
[41] user typed 2026-11-10 06:58: use Postgres for the queue, not Redis. we already run it
[42] assistant 06:59: I'll move the queue to Postgres with SKIP LOCKED. Shall I drop the Redis client?
[43] user typed 07:00: yes
[44] tool 07:02: ran `go test ./internal/queue` (exit 1) | failure: "undefined: redis.Client"
[45] user chose 07:03: Q: "Keep the old consumer?" -> "Remove it"
[46] assistant [after: web] 07:05: the docs say advisory locks are cheaper …
```

The label comes from the event's `pv` and kind, and the bracket after `assistant` from its `taint`. A line inside an entry that looks like an entry is quoted and continuation lines are indented, by the same cleaning the brief uses (update-model must-keeps 12 and 13). Redaction runs again on the way out.

### 5.3 What the model returns

Delta operations only. An item, a next step or a question that is not mentioned is kept.

```json
{
  "workstream": {"use": "w01k2m3n4p5q6a", "title": null},
  "ops": [
    {"op": "item.add", "id": null, "kind": "decision",
     "text": "The job queue runs on Postgres with SKIP LOCKED, not Redis.",
     "why": "Postgres is already in the stack.", "steps": [],
     "ev": [{"e": 41, "q": "use Postgres for the queue, not Redis", "as": "said"}],
     "scope": "project", "paths": ["internal/queue"], "cues": ["queue backend", "redis client removed"],
     "admit": "decision"}
  ],
  "nothing": null
}
```

| Field | Values | Notes |
| --- | --- | --- |
| `workstream.use`, `.title` | a handle the input showed, or null with a title of at most 60 characters | ignored when code binds the session to a workstream (6.6) |
| `op` | `note`, `status`, `next.add`, `next.done`, `next.drop`, `ask.add`, `ask.answer`, `ask.drop`, `item.add`, `item.supersede`, `item.retire`, `item.confirm` | |
| `id` | a handle the input showed; null for an add | the model never writes a new id (update-model must-keep 21) |
| `kind` | for `note`: `did`, `found`, `failed`, `left`. For items: `fact`, `decision`, `constraint`, `procedure`, `lesson`, `preference` | |
| `text` | one plain sentence, at most 300 characters | |
| `why` | the reason of a decision, the cause in a lesson, the reason of a drop or a retire; null otherwise | the field v1 has no place for (PGS-09) |
| `steps` | at most 12 short lines: a procedure's steps, a lesson's fix | |
| `ev` | 1 to 3 of `{e, q, as}`: entry number, the exact words, and `said`, `assent`, `proposal`, `result` or `report` | `q` is 8 to 240 characters, the store's excerpt bound |
| `scope` | `here` (this workstream), `project`, `all` | code may narrow it (6.5) |
| `paths`, `cues` | at most 5 each | `cues` are the write-time keys the retrieval core indexes: situations, aliases, identifiers |
| `admit` | `instruction`, `decision`, `invariant`, `environment`, `pointer`, `failure`, `command` | required for `item.add` (5.9) |
| `nothing` | `none_new`, `only_progress`, `only_derivable`; required when `ops` is empty | makes "wrote nothing" a counted answer |

At most 24 operations an episode. Every key is required and nullable, no object takes another key, and there is no `maxLength`, as both hosts' strict modes want (update-model must-keep 20). The answer is parsed strictly again in code.

There is no summary and no "last chat" field. Where a workstream stands is one `status` operation of at most 240 characters that cites its spans like any other line and is shown the previous status, so it is maintained and not rewritten blind (PGS-11, UPW-12). What a chat did is its `note` lines.

### 5.4 Evidence: exact words, checked by code

- `ledger.Verify(event, q)` must find `q` in `ledger.Render(event)`. The only normalization, on both sides, is Unicode NFC and collapsing each run of white space to one space. Anything else is `quote_not_found`.
- The store keeps the verified span as the excerpt, through `ledger.Excerpt`, with the event id and a hash (store 5.3). A statement can then be read against its source on any machine.
- This replaces every test of the form "is the model's text close enough to the user's": `usersWords`, `inOwnWords`, `samePolarity`, `restsOn`, `nearKnowledge`, the stoplists, the contraction table and the scope phrase lists (UPW-03, PGS-12). It works the same in any language (5.10).

A quote proves the span exists and who wrote it. It does not prove the statement follows from it. Three things bound that risk: the grounding check (5.7, step 6), the delivery of a user's standing instruction together with the user's own quote (contract D-R2), and the fidelity measures of section 1, which score values, not codenames.

**A measured fallback.** Small models sometimes alter a quote. DV-03 measures `quote_miss` after one repair on both hosts. If it is over 2%, the contract switches to segment ids: the renderer numbers the segments of each entry (split at line breaks and at sentence-final punctuation of any script), the model cites `[41.2]`, and code copies the text. That gives up the model's own proof that it read the words and removes the miss. The schema changes in one field.

### 5.5 Tags come from the record

The tag of an operation is computed by code from the events its spans sit in. The model's `as` only says which span is the proposal and which the assent; where it disagrees with the event, code corrects it and counts `role_fixed`.

| The span sits in | `pv`, kind | Taint | Supports |
| --- | --- | --- | --- |
| a typed prompt, outside its pasted parts; a typed dialog answer | `typed` | any | `stated` |
| a dialog answer picked from options, or a plan approval | `chosen` | none | `accepted`; the proposal is the option or plan text in the same event |
| a typed prompt that is the user's first prompt after the agent event holding the proposal span | `typed` after `agent` | none on the proposal's turn | `accepted` |
| the same, the proposal written in a tainted turn | | `web`, `net`, `mcp`, `subagent` | `accepted`, scope `here` only, flagged `outside` |
| a tool record's own fields or failure digest | `tool` | tool class is not fetch or mcp | `observed` |
| an agent reply, segment or subagent report | `agent` | none | `suggested` |
| agent text | `agent` | `web`, `net`, `mcp`, `subagent` | `external` |
| agent text | `agent` | `memory` (eva-md delivered or was asked in or before the turn) | `echo`: adds no support and creates nothing |
| a pasted part, a program's or the host's prompt, `unknown` | | | `external` |

Order: `stated` > `accepted` > `observed` > `suggested` > `external`. v1's `inferred` has no place: every operation has evidence or is refused. It survives only as a read-only value on migrated items.

Three things follow.

- **Observed means a tool said it.** v1 grants `observed` for citing any tool entry or the assistant's message after one (PGS-03). Since tool output is not captured, what the agent reports about a tool is `suggested`. Only a failure digest or a record's own fields are `observed`.
- **Taint is wider than three tool names** (UPM-12). It is computed by the ledger from typed tool records: web tools, network commands, MCP tools of other servers, subagents and eva-md's own deliveries.
- **Memory cannot confirm itself** (MCP-12). A restatement of what eva-md put into the chat is `echo`.

### 5.6 Acceptance per product

There are three products, not four. A standing rule is an item (6.1).

| Product | Operations | Accepted when | Otherwise |
| --- | --- | --- | --- |
| **Log** | `note` | any tag | `external` notes are kept and labelled; they record what happened |
| **State** | `status`, `next.*`, `ask.*` | add: any tag but `external`. Done: a `result` span in a tool record with status ok, or the user's words. Answer: the user's words or a result. Drop of a user-tagged entry: the user's words (pages must-keep 4) | mended down, as v1 does: the tag falls to what the spans support |
| **Items** | `item.*` | by kind, below | a candidate, a dispute, or a counted refusal |

| Kind | Becomes active with | Otherwise |
| --- | --- | --- |
| `decision` | `stated` or `accepted` | `suggested`: filed as a question, "Proposed: …", as v1 does |
| `constraint`, `preference` | `stated` only. A standing instruction is the user's own words (update-write must-keep 7) | `accepted`: a candidate, shown when asked, active when the user confirms or says it |
| `fact` | `stated`, or `observed` | `suggested`: a candidate; active after support from 2 independent sessions, or (Stage 3) when its anchors check out against the code |
| `procedure` | `stated` or `accepted`; or `suggested` when each step's command matches a `run` record with status ok, in order, in the episode | a candidate |
| `lesson` | `suggested` or better, and a `result` span in a failed tool record. The fix may cite a later ok record | written as a `failed` note only |

Each operation is judged alone. A refused operation is counted by code and never takes another with it. An episode's range is covered `done` whatever was refused, with the refusal counts in the journal. The only whole-answer failure is an answer that is not the schema's JSON, which is class `answer_invalid` and goes to the repair round. The thresholds that reject a whole v1 answer (more than half the ids without an action, more than three undated lines, a sixth knowledge change dropped) are gone (UPW-07, UPW-06, PGS-02).

### 5.7 The checker

Pure and deterministic, like v1's `Validate` (update-write must-keep 17). Steps for each operation, with the refusal code each can give:

| Step | Check | Code | Repairable |
| --- | --- | --- | --- |
| 1 | shape and bounds | `bad_shape` | yes |
| 2 | target handle was shown and is writable | `unknown_target`, `read_only_target` | first only |
| 3 | every span: entry in range, quote found | `bad_entry`, `quote_not_found` | yes |
| 4 | tag from the spans (5.5); control-token and forged-markup text | `control_text` | no |
| 5 | acceptance by product and kind (5.6); for a supersede or retire, the lattice of 6.4 | `tag_too_low` (or mended to a candidate or a dispute) | no |
| 6 | grounding: every path, URL, commit id, dotted identifier and number with a unit in `text`, `why` and `steps` appears in a cited event, in the target item or in a shown related item | `ungrounded` | yes |
| 7 | dates: a date in the text appears in a cited event or in the target item | `ungrounded_date` | yes |
| 8 | freshness: a change to an existing entry needs evidence at or after that entry's own time | `stale_evidence` | no |
| 9 | duplicates: an add equal or near-equal to an active item of the project becomes a confirm of it, with the new span as support; from an `echo` span it is dropped | `echo` | no |
| 10 | admission (5.9) | `no_admission`, `in_instruction_file` | first only |
| 11 | redaction again on every text; a credential-shaped value | `secret` | no |

Steps 6 and 7 are v1's grounding and date rules with the two faults the verifiers found repaired: the haystack now holds the item being changed and the related items shown, so a correction that keeps a path from the statement it replaces is no longer refused (UPM-02's fourth correction), and a date the entry already holds is no longer treated as invented (UPW-14). Both use token classes found by pattern, not word lists.

`Guarantees`, the black-box check the accuracy suite runs on every validated pass, is rewritten for v2 with the same role: whatever the model wrote, no user-tagged entry was created without a typed span, none was changed or retired without one, nothing done lacks a result, every date written is in a cited event, and no id left the store.

### 5.8 One repair round

When at least one operation of the state or item products was refused with a repairable code, the worker makes one more call. Its input is the first call's input, byte for byte, followed by the model's own operations and a list of `#index code place` lines. The model returns replacements for those indexes only, or `skip`. Refusals after the round are final for this contract version and are counted.

The repair input shares its whole prefix with the first call, so on a host that caches prompts it is mostly cached input. Whether the headless path does cache it is measured in DV-03; if it does not, repair is limited to refused user-tagged operations.

### 5.9 Admission: default to nothing

- "No operation" is a valid answer and the expected one for most short chats. `nothing` says why.
- An `item.add` must name why the repository cannot say it: an instruction of the user's, a decision with its reason, an invariant across files, an environment quirk, a pointer to something outside the repository, a failed approach, or a non-standard command. Anything else is `no_admission`.
- Code checks an add against the instruction files the hosts already load for that project (`CLAUDE.md`, `AGENTS.md` and the files they import, read with bounds and without following a link out of the repository). A near-equal line is `in_instruction_file`.
- Nothing is refused because a host's own memory holds it: the other host and other machines do not have that memory. Not repeating it in a chat where the host already loads it is the deliver area's decision at delivery time.
- No overview of a repository, a module or a file is ever an item.

### 5.10 Language neutrality

Acceptance uses no stopword list, no negation list, no phrase list and no stemmer. What remains is language-neutral by construction: exact quotes, provenance fields, token classes found by pattern (paths, identifiers, numbers) and time order. Scope `all` is the model's reading of the user's words, accepted only on a `stated` span and stored with that quote, so a wrong reading is visible where the rule is shown. The translated twins of the fact scripts are the test (section 1).

---

## 6. The semantics of the memory model

The store owns files, ids, merging and history. This section defines what the fields mean. The table of kinds, states and tags is this area's and is versioned with the store's schema (store contract S-D4).

### 6.1 Three products

| Product | What it is | Ordering rule | Replaces |
| --- | --- | --- | --- |
| **Log** | per session, append-only: a code-built trail and cited note lines | none: lines only append | Timeline, Done, last chat |
| **State** | per workstream: one cited status line, next steps, open questions | per entry, by evidence time: an operation older than the entry's last change is ignored as `stale_op`. A late episode (older than the workstream's newest by a day or more) adds notes and items but no next steps | the Now, and the page-wide `as_of` guard |
| **Items** | durable statements with a lifecycle | per item, by its own times | Decisions in force, rules pages, knowledge pages |

Rules and knowledge no longer depend on a thread page's state (UPW-06), and nothing compares a chat with one clock for a whole page, which is what would break when two machines write (STO-04).

A standing rule is not a fourth store. It is a `constraint` or a `preference` under the `stated` tag, and "Rules" is a view. That also ends the two unreconciled homes of constraints (SYS-08).

### 6.2 Items

| Field | Meaning | Set by |
| --- | --- | --- |
| kind | `fact`, `decision`, `constraint` (must hold), `procedure`, `lesson` (tried, failed because, fix), `preference` (should), plus the working kinds `next` and `question` | the model, within 5.6 |
| statement, `why`, `steps` | the headline, the reason or cause, the steps | the model, checked |
| tag | 5.5 | code |
| scope | `here`, `project`, `all` | the model, narrowed by code |
| provisional | the workstream and branch a `here` item waits on | code (6.5) |
| evidence | up to 3 excerpts with event id, voice and hash | code, through the ledger |
| learned | time of the first evidence | code |
| confirmed | time of the newest support: a new span, a person's confirm, or a use the agent validated | code |
| support | independent sessions that gave a span, excluding `echo` and `external` | code |
| supersedes | the item this one replaces | code, from `item.supersede` |
| anchors | `repo@commit:path`, from `paths` and the episode's captured head | code |
| cues | write-time keys for retrieval | the model; a cue that is only code-like tokens absent from the evidence is dropped |
| state | below | code |

Kept out of the committed file, in local state, as the store design argues: drift of the anchors against this machine's clone, and use counts from the deliver area's delivery records.

`lesson` and `preference` are new (PGS-09). `entity` is not adopted: a system or a service is a scope and an anchor, not a kind, and nothing measured asks for a graph.

### 6.3 Lifecycle

```
candidate ──tag rule met, support reached 2, or a person confirms──► active
candidate ──90 days without support──► expired
active ──item.supersede with authority──► superseded        (the successor names it; nothing is deleted)
active ──item.retire with authority──► retired
active ──a contradicting change without authority──► active, disputed   (both sides kept and shown)
active ──anchors gone at the default branch's head──► active, stale      (label and hold-back are local and computed)
active ──not user-tagged, no delivery, use or support for 180 days──► archived
any ──forget──► retracted                                      (the store's cascade)
```

`disputed` and `stale` are flags, not ends. A stale item is not delivered unasked and is returned with its label when the agent asks. A user-tagged item is never archived or retired by code; it is at most labelled old.

### 6.4 Who may change what

| The existing item is | `stated` evidence | `accepted` | `observed`, untainted | `suggested` | `external`, `echo` |
| --- | --- | --- | --- | --- | --- |
| `stated` or `accepted`, or any `decision`, `constraint`, `preference` | supersede, retire | supersede a `decision` only | dispute | dispute | nothing |
| `observed` | supersede, retire | supersede | supersede | dispute | nothing |
| `suggested` (a candidate) | any | any | any | supersede | nothing |
| locked or verified by a person | dispute | dispute | dispute | dispute | nothing |

Every change also needs evidence at or after the item's own time, so an older chat processed late never overrides a newer one (pages must-keep 4).

This table is the fix for PGS-07. In v1 a knowledge supersede tagged observed skips the words test, can replace what the user stated, can turn a constraint into a fact, and is logged as `stated`; the repository's own test asserts it. Here the same operation is mended into a dispute, and the trail carries the operation's own tag.

A change the model proposes but may not make is not thrown away: it becomes a dispute beside the item, which the deliver area shows as "an observation on <date> suggests otherwise". A person's protected item gets the same treatment in place of v1's skip of the whole batch (PGS-05).

### 6.5 Branch-provisional knowledge

A `fact`, `decision` or `procedure` learned in a session whose branch is not the project's default branch, and whose captured head is not an ancestor of it, is scope `here` and provisional on that workstream, whatever scope the model gave. It is delivered inside that workstream. `constraint`, `preference` and `lesson` are not provisional: a standing instruction of the user's holds wherever it was said, and a failed approach stays failed.

Housekeeping resolves it with one hardened, read-only git call, never in a hook: when the branch's head is an ancestor of the default branch, or the branch is gone and its last captured head is, the workstream is merged and its provisional items become project items. When the branch is gone and was not merged, the workstream is abandoned: its provisional decisions and facts are retired with that reason, and its lessons are kept.

### 6.6 Workstreams

A workstream replaces the branch-keyed thread (PGS-01, PRD-06, UPW-15).

| | |
| --- | --- |
| Identity | an id the store mints; the branch is an attribute |
| States | `active`; `paused` after 14 days without an episode; `merged`; `abandoned`; `archived` 90 days after any of those three |
| Transitions | by code, in housekeeping (6.5) |
| Fold at merge | user-tagged decisions become project items; open next steps and questions are closed with reason `merged`; the log stays with the sessions |

**Binding a session to a workstream**, in order:

1. A session on a non-default branch belongs to the workstream bound to that project and branch, created at first sight with the branch name as its title. The model's choice is not asked for. This is v1's proven unit, kept where it is right.
2. A session resumed, forked or compacted from another inherits its parent's workstream (the ledger's lineage).
3. Otherwise (the default branch, a detached head, a folder outside git) the fast pass is shown at most 7 active or paused workstreams of the project (handle, title, last active, top paths) and returns `use` or a new title. Code accepts only a handle it showed. A long chat on the default branch may move between workstreams from one episode to the next.

Rule 3 is the only place a model decides structure, and it is bounded to a choice among shown handles. The task-switching study measured 12 to 14 points lost to interleaved work and most of it recovered by routing each task its own history; the evaluate area's branch-switching suite, in its default-branch variant, measures whether rule 3 routes well. If routing accuracy is under 90%, the fallback needs no model: one workstream per session lineage, merged by code with another of the last 7 days when their edited files overlap by half or more.

Every chat outside a repository no longer shares one page: the ledger gives each folder its own project, and rule 3 applies inside it. A rule said in a scratch folder is scope `project` unless the user's words say otherwise (UPW-15).

### 6.7 The episode's log

Per session, written by the fast pass into the store's episode file:

- **Trail**, built by code from typed tool records, no model: files edited (at most 20), commands run with their status (the last 5), how the last turn ended, and `checked: yes` only when a command with status ok ran after the last edit. The handoff studies name validation evidence as the most useful part of a note and the artifact trail as the weakest part of every model-written summary; both are computed here.
- **Notes**, from the model, each cited: `did`, `found`, `failed`, `left`.

The episode file's `Summary` section in the store design is this trail. No free-text summary is written.

### 6.8 System pages

The evidence is against the category. Model-written repository overviews did not improve task success and raised cost by a fifth; overviews did not shorten the path to the right file; self-written skills made both hosts worse. A system page is a model-written overview, created by a prose procedure, that then reports its own decay (SYS-01, SYS-03, MCP-13). Nothing has measured one helping (SYS-02).

**Recommendation: freeze them (DV-D6).**

- Existing pages stay where they are, readable and searchable. `eva-md system write` keeps working when a person asks for a digest, with the store's real compare-and-swap (SYS-10).
- No new investment: no refresh queue, no bootstrap, no claim-level sources, no map rebuild. The audit's direction for SYS-01, a model-run refresh of pages from diffs, is declined.
- Not delivered unasked by default. The chat-start and per-prompt system pointers become an evaluation arm. If the arm shows a gain, they return; if not, the page type is retired in a later release with the files left in place.
- What is worth having from a system page arrives through the normal path as items: a constraint the user stated about a service, a decision with its reason, an invariant across repositories, each cited and anchored.
- An agent-written `repos` list no longer widens whose knowledge is recalled where (SYS-11). Existing sibling relations are kept and marked unconfirmed; new ones need the person (the store's `shares_with`).
- `internal/coderef`'s hardened git runner and its honest "not measured" semantics are kept and reused for item anchors.
- The daily lint (SYS-07) is absorbed: reference resolution is the ledger's and the store's, page size has no successor because nothing grows without bound, and "detect, then tell a person" becomes housekeeping that repairs.

---

## 7. The tiers and what each writes

| | Capture time (the ledger, no model) | Fast pass (small model, per settled episode) | Housekeeping (code, per tick) | Review (stronger model, gated) |
| --- | --- | --- | --- | --- |
| Serves | the brief, within seconds: the session manifest and the raw tail | state, log, items | the health of the corpus | what code cannot judge |
| Log | | the trail and the notes | | |
| State | | status, next steps, questions, the workstream binding | lifecycle transitions; next steps of a paused workstream older than 30 days closed into the log | |
| Items | | adds (active or candidate), supersedes and retires inside 6.4, confirms | H1 to H8 below | merges of near-duplicates, disputes among items below the user's tier, judged promotions |
| Commit | none | one per episode | one per tick, when anything changed | one per review |

The brief does not wait for any model: the ledger's manifest and tail are enough for a chat that starts the second the last one ended (product must-keep 4). The fast pass is not "a model call on every turn", which decisions.md rules out: it runs per settled episode, at a boundary or after quiet.

**Housekeeping steps**, each deterministic, each through `store.Apply` with `process:eva-md` as the actor:

| | Step |
| --- | --- |
| H1 | Exact duplicates (same kind and scope, statements equal after normalization) merge into the older item; evidence is united |
| H2 | A candidate fact with support from 2 independent sessions becomes active |
| H3 | Merged and abandoned workstreams are folded (6.5, 6.6) |
| H4 | Drift is recomputed for anchored items against the default branch's head, into local state; an item whose every anchor is gone is flagged stale |
| H5 | Archive by disuse, never a user-tagged item (6.3) |
| H6 | Candidates expire at 90 days |
| H7 | A workstream-scoped preference with support from 2 sessions becomes a project preference |
| H8 | Two user-tagged items of one kind and scope that the retrieval core scores as near-equal: the older is flagged disputed by the newer. Nothing is superseded; the newest is preferred in delivery and both carry the label |

**Review** is the lead's "consolidation". It is the last thing to build and may never be needed (DV-D8). Its input is clusters of near-equal items, open disputes and candidates that code could not promote, with statements and the excerpts already stored. Its output is the same operation vocabulary plus `merge`. The checker applies the same lattice: a review may not create text whose grounded tokens are absent from the items it merges, and may not change a user-tagged item at all. `store.Plan` is the lint gate: a review that would touch more than a fifth of a project's active items, lose an id, or change a protected item is discarded and journaled. One commit; one `eva-md undo` reverses it. There is no candidate branch or worktree: the model never writes files, so the candidate is the planned transaction.

Evidence excerpts from one host's chats are sent only to that host's model. A cluster that mixes both hosts' evidence is reviewed on statements alone, which both hosts already receive in briefs.

---

## 8. The model runner

### 8.1 Interface

The seam exists in v1 (`hostCLI{efforts, run}`), as the UPM-09 verifier noted. It grows two things: a probe and a typed outcome.

```go
type Runner interface {
    Probe(ctx context.Context) (Caps, error)      // no model call; cached by CLI path, size, mtime and version
    Run(ctx context.Context, c Call) (Outcome, error)
}
type Call struct {
    Task     string        // extract | repair | review | livecheck
    System   string        // the contract's instructions
    Input    []byte        // on stdin, never in an argument
    Schema   []byte
    Model, Effort string   // from the policy table; empty means the host default
    Deadline time.Duration // one deadline for every attempt of the call
    RunDir   string        // an empty folder, checked
}
type Outcome struct {
    Class         string   // ok | auth | rate_limit | host | timeout | answer_invalid | tool_used | cli_missing
    Answer        []byte
    ModelAsked, ModelAnswered, Effort, CLIVersion, Billing string
    Usage         struct{ In, CachedIn, Out int }
    Quota         struct{ Known bool; RemainingPct int; ResetAt time.Time }
    Signals       []string // names of structured fields and event types seen, never text
}
```

Kept unchanged from v1, with their tests: input on stdin only; an empty run folder; an allowlisted environment with `EVA_MD_INNER=1` and the host's own memory off; no transcript left; the whole process group killed on timeout or cancel; the exit status taken from the process state; the Codex answer file in a private folder, read without following a link; an answer printed before a timeout not counted; one deadline for all attempts; the recorded absolute CLI path (update-model must-keeps 1 to 11).

Changed:

| Change | Finding |
| --- | --- |
| Classes come from structured fields first. Unrecognized output is `host`, not `invalid_output` | UPM-03 |
| On Claude the run reads the event stream and checks the opening message: an empty tool list, the model, the key source. Any tool-use block rejects the run. The answering model is recorded | UPM-14 |
| On Codex an event type the reader does not know is `host` (this line first said it was ignored unless it was an item; the plan's row DV-11 and its card rule); an unknown item type is `host`; a known tool item is `tool_used`. The feature list is read once per CLI version, not before every attempt | UPM-03, UPM-09 |
| Flags and effort levels come from the probe, not from a daily ladder of refusals | UPM-09 |
| A killed run reports what usage it saw | UPM-07 |

### 8.2 Backends

| Backend | Status | Notes |
| --- | --- | --- |
| The chat's host CLI, on the plan login | the default | `claude -p` and `codex exec`, sealed as today. Text goes only to the provider the chat already used |
| The same CLI on an API key | opt-in (DV-D3) | `organize.<host>.billing = "api_key"` with a `key_command` that prints the key; eva-md never stores it. This is the answer if `--bare` becomes the default of `claude -p`, for Bedrock and Vertex setups, and for plans whose monthly API credits cover `claude -p`. It reuses the sealed run; eva-md gains no HTTP client of its own |
| A Claude mod's in-session model call | not in v2 | runs only while a chat is open; the API changed in most releases since it shipped; it can be switched off remotely and is partly blocked on team logins |
| Direct calls on the ChatGPT plan through sign-in | not in v2 | a preview with unconfirmed eligibility |

### 8.3 Policy for model and effort

The policy is a data table shipped with a release, keyed by host and CLI version range, because model names move (Codex changed its memory models' defaults in its last release).

| Task | Claude Code | Codex | Effort | Deadline |
| --- | --- | --- | --- | --- |
| `extract`, `repair` | the small alias (`haiku`) | the small model its own memory writer uses for extraction | low | 120 s |
| escalation, one step | `sonnet` | the model its memory writer uses for consolidation | medium | 180 s |
| `review` | `sonnet` | the same consolidation model | medium | 300 s |
| any, when the named model is refused | the host's default model, then the chat's model | the same | low | |

These are starting points. DV-03 runs the fact scripts on each host at three model tiers and two efforts and records fidelity, tokens and seconds. The default is the cheapest row with `fact_recall` at least 0.90, `wrong_write` at most 0.02 and `misattributed` 0. If no small row passes, the default is the middle tier. v1's `max` and `ultra` are never the default. A person may still pin a model or an effort per host, as v1's effort key allows.

By the published credit prices, Codex's small model costs a tenth to a twentieth of its larger ones per token; the gap on Claude and the effect of effort on output tokens are what DV-02 measures.

---

## 9. Cost accounting

- Every call's usage is in the journal: tokens in, cached in, out; calls; milliseconds; task; host; billing; the model asked and the model that answered; the yield (operations proposed, applied, refused, or `nothing`).
- `work.Cost(day, host)` gives the hosts area one number for `status` ("memory used about N thousand tokens today, X% of its budget") and gives the evaluate area the background part of "net tokens per completed task", with cached and uncached input apart.
- The budget is in weighted tokens, not calls. v1 counts passes, and a pass may be several CLI runs down the effort ladder (the UPM-05 verifier's note).
- Reported per week, content-free: tokens per organized chat, the share of episodes that answered `nothing`, the repair rate and its yield, the escalation rate, refusals by code. These are the numbers the validator's cost was never given (UPW-02).

---

## 10. Kept, rebuilt, removed

### 10.1 Kept as code

`internal/update/process.go` (the sealed child run), the environment allowlist, the run-folder check, the Codex answer-file handling, `fake.go` and the fake-CLI test rig, `live.go` as the known-good fixture, the anti-forgery rendering in `internal/freetext`, `internal/coderef`'s git runner and clone confirmation, `internal/systempage`'s write checks, and the accuracy package's fact scripts as the seed of the fidelity suite.

### 10.2 Invariants

| Invariant (audit's must-keep) | In v2 |
| --- | --- |
| Hooks never wait and never share a lock with a job; a failed spawn is ignored (update-write 1) | kept: one detached spawn after the output |
| The inbox watermark is the truth about pending work; a queue may only be a hint (update-write 2) | kept in substance: pending is the ledger minus the store's coverage; marks and the journal are hints |
| Progress moves exactly past what a pass covered, only after a validated save; an unwritable marker costs no call (update-write 3) | kept: coverage lands in the commit that holds the operations; the journal's start line gates the call |
| Save order knowledge, rules, thread, for a safe rerun (update-write 4) | changed: one transaction, exactly once per pass id. Stronger |
| People's pages are never overwritten; an untagged entry is the user's (update-write 6) | kept, per item and in the store; a protected item turns a change into a dispute instead of skipping the batch |
| Only stated and accepted make a decision; pasted text never counts; an event or a host prompt is never the user (S1) | kept, and decided by `pv` instead of by word overlap |
| A change to what the user owns needs the user's prompt at or after the entry's time (S4) | kept, per entry |
| No id is lost; nothing is silently merged | kept; a merge unites evidence and keeps both ids resolvable |
| Model output is hostile text: redacted again, capped, escaped (update-write 10) | kept |
| Logs hold counts, names and reasons, never text (update-write 11) | kept for the journal. `pending/<pass>.json` holds memory text for the seconds between validation and commit, in the 0600 state folder |
| Host failures never count against a session; a person's run overrides and clears holds (update-write 12) | kept, and widened: unknown failures are the host's by default |
| Program-run chats cost no call; history from before organizing is never worked through by surprise (update-write 13, update-model 24) | kept |
| The dry run is side-effect free and byte-stable; the checker is pure (update-write 14, 17) | kept |
| A prompt waits for its reply; a cut group is continued (update-write 16) | kept, stronger: no cut inside a turn |
| The sweep's day is the hook's day (update-write 18) | gone with the sweep |
| One strict schema both hosts accept; the model writes no ids, dates, markdown or provenance (update-model 20, 21) | kept |
| Always the chat's host, on the subscription login (update-model 22) | changed: still the chat's host; not the chat's model; an API key is an opt-in. Reopens decisions 5 and 17 (DV-D2, DV-D3) |
| What a web fetch reported never becomes observed knowledge (update-model 17) | kept and widened to the ledger's taint classes |
| Caps never drop a decision (pages 5) | kept: there is no cap on decisions; what is shown is the deliver area's budget |
| Every supersede and drop leaves a trail (pages 15) | kept: the successor names its predecessor, in one commit, with the operation's own tag |

### 10.3 Rebuilt, and what a smaller change would leave

| Rebuilt | Resolves | A smaller change, and what it leaves |
| --- | --- | --- |
| The control plane: one worker, computed pending work, the journal, ripeness, priorities, gates, breaker, dead letters | UPW-04, UPW-01, UPW-09, UPW-13, UPW-10, UPM-03, UPM-07, OPS-08, EVL-07 | Leave the spawn-per-trigger jobs and add a "run again" mark for a busy lock. That fixes lost triggers and nothing else: no priority, no recorded refusals, no breaker, the sweep and the log scans stay |
| The input model: episodes and a cursor on ledger events | UPM-04, UPM-15, UPM-06 (with the shared retrieval core) | Cut v1's chunks at turn ends. That fixes mid-turn cuts and keeps the rescans, the lost middle of long prompts, commits read at job time and the 12 rows by word count |
| The extraction contract: delta operations, exact spans, tags from provenance, per-operation acceptance, one repair round | UPM-02, UPM-10, UPM-11, UPM-12, UPM-08, UPW-03, UPW-06, UPW-07, UPW-12, UPW-14, PGS-02, PGS-07, PGS-12, MCP-07, MCP-12 | State the hidden rules in the prompt and add a repair turn. That may halve refusals; it keeps word overlap as the judge of who said what, in English, keeps "yes, do that" out of durable memory, and keeps the observed-over-stated hole |
| The memory semantics: three products, items with a lifecycle, the lattice, provisional knowledge, workstreams | PGS-01, PGS-03, PGS-09, PGS-11, PGS-13, PRD-03, PRD-06, UPW-08, UPW-15, STO-04 | Add a `state` field to thread pages and a merge check. That marks merged branches and leaves one Now per branch, one page for all work outside git, the 40-decision cap and facts without validity |

### 10.4 Evolved, not rebuilt

| Evolved | Resolves |
| --- | --- |
| The runner seam: probe, typed outcomes, stream reading on Claude, a policy table | UPM-09, UPM-14, UPM-05, PRD-12 |
| Cost: the run log's per-pass figures become a ledger with a token budget | EVL-08, UPW-02 |
| System pages: frozen | SYS-01 to SYS-14 are answered by not investing, except SYS-10 and SYS-11, which the store closes |

### 10.5 Removed

- `internal/update`'s `validate.go`, `rules.go`, `knowledge.go` and `tokens.go` as they stand: about 2,100 lines whose main job is matching the model's text to the user's.
- `input.go`'s prefix scan and markers, `backlog.go`, `baseline.go` (its record becomes coverage rows), `job.go`'s sweep, `runlog.go`'s derived state, the retry ladder in `runner.go`, most of `classify.go`'s phrase lists.
- `internal/hook/trigger.go`'s thresholds and the per-day markers in `internal/hook/maintain.go`; `cmd/eva-md/maintain.go` as a job of its own; `internal/lint`.
- The schema's `keep` action, the parser for answer shapes of rules versions 1 to 3, the length pin on the prompt.
- The quarantine that ends in a skipped chunk.

---

## 11. Host facts this design leans on, and how each is confirmed first

All from documentation and source read on 2026-11-10. DV-02 and DV-03 record each on a real host before anything in Stage 2 is built on it. Both need the owner's leave, because they make model calls.

| Fact | Used for | If it is false |
| --- | --- | --- |
| `claude -p` from a detached job, on the plan login, takes `--safe-mode --tools "" --strict-mcp-config --no-session-persistence --json-schema`, a model alias and `--effort low` | the default backend on Claude | the API-key backend; else the chat's model at low effort |
| Claude's event stream carries an opening message with the tool list, the model and the key source, and the structured answer | the checks of UPM-14 | keep the single envelope, read `permission_denials`, and record the keys of `modelUsage` as the answering model |
| `--bare` is not yet the default of `claude -p`, and `--safe-mode` keeps the login | the default backend | the probe sees the change in the help text and version; the breaker opens on `auth`; DV-D3's backend takes over where a key is set |
| Either host exposes its remaining allowance to a headless run | the quota gate's 25% rule | the budget, the rate-limit hold and the idle gate only |
| `codex exec --ephemeral --output-schema` with a named small model and `model_reasoning_effort=low` runs on the plan; which effort names it accepts and how it words a refusal | the default backend on Codex | the host default model with no effort flag |
| A Codex run with tool features disabled has no callable tool, and does not load the user's global instructions | the sealed run | the reader still kills on a tool item; add `--ignore-user-config` by default |
| Codex hooks see the environment of the `codex` process, so `EVA_MD_INNER=1` reaches eva-md's own hooks in a headless run | no capture of our own runs | the run folder is a temporary folder, which the hooks already skip |
| Both hosts enforce the schema's array bounds, enums and nullable types | parse failures are rare | the repair round |
| A repair call's shared prefix is read from the prompt cache on the headless path | the cost of repair | repair only user-tagged refusals |
| A detached child of a hook that exits in time survives (read in Codex's source; relied on by v1 on Claude Code) | wakes | the tick alone; `end_to_saved` rises to the tick interval |
| Claude Code gives plugin hooks 1.5 s in all at session end; Codex fires its end when a chat closes or after 30 idle minutes | the `ended` rule | the `idle` rule covers it |
| A launchd user agent may be installed on the machine | the tick | the fallback of 3.8 |

A canary keeps these current: on every new host CLI version the probe sees, the worker runs the known-good live fixture once before any user episode (the half-open path of the breaker). A host change is then found by a call that cannot be the content's fault.

---

## 12. Migration: a user's wiki and inbox survive

The ledger migrates the inbox and the store migrates the files. This area supplies the meaning, and migrates its own state.

1. **Progress.** v1 watermarks become coverage rows `done` under contract version 4, through the ledger's conversion to sequence numbers and the store's frozen v1 table. Nothing is organized twice. A session with a v1 backlog becomes pending work for the new worker.
2. **What v1 passed over.** v1's run log records that a chunk was skipped or that lines were refused, but not which lines. Those ranges cannot be marked refused. They are reachable by the opt-in replay of step 6.
3. **Job state.** v1's run log is read once for a cost baseline, then left for 14 days. Quarantines, holds, backoffs and the sweep marker are not carried over.
4. **The semantic mapping** the store's migrator applies:

| v1 | v2 |
| --- | --- |
| A thread page | a workstream bound to that branch; `merged` or `abandoned` by the git check of 6.5 at first housekeeping. Its summary becomes the status line, tagged `suggested` |
| Next steps, open questions | working items with their v1 tag and references |
| Decisions in force, including the over-cap lines parked under Open questions (PGS-13) | `decision` items; on the default branch scope `project`, otherwise provisional |
| A rules page line | a `constraint` item, tag `stated`; the project's page gives scope `project`, the general page scope `all`. The store draft's "kind rule" is this |
| A knowledge page | an item of the same kind and scope, still active. Tag: `stated` and `accepted` are kept; `observed` is kept only when a reference resolves to a tool entry, and is otherwise `suggested`, because v1's observed could rest on the assistant's report (PGS-03) |
| `suggested`, `inferred` | `suggested`; `inferred` kept as a read-only legacy value |
| Timeline, Done, last chat | the frozen episode archive of the workstream, byte for byte |
| Locked and verified marks | carried to every item made from the page |
| System pages | unchanged |

   Migrated evidence is a v1 reference, which the ledger resolves for good; it has no verified span. Such items are marked `evidence: legacy`, and nothing downgrades them for it.
5. **No model call** is made by migration, and no item is created that v1 did not hold.
6. **Opt-in replay (DV-D7).** `eva-md work --replay --project P --since DATE --estimate` prints a token estimate from the rendered size, then, if confirmed, runs the v2 contract over the ledger for that project, newest first, inside the P6 budget. Duplicates of migrated items become support, which also gives migrated items their first verified spans. This one mechanism is the dead-letter replay, the answer to "re-run when the rules improve" (PRD-03), and the bounded backfill (UPM-13).
7. **Cut-over** happens with the store and the deliver area behind one flag (the store design's point 9). Before it, the new worker runs in shadow against a scratch store, on fixtures and, with the owner's leave, on the real ledger with canned or live answers; only counts leave that run.

---

## 13. Alternatives weighed

| Question | Options | Choice and reason |
| --- | --- | --- |
| Control plane | (a) v1 plus a re-run mark; (b) one worker over computed pending work; (c) a resident daemon; (d) an embedded queue database | (b). (a) is 10.3's smaller change. (c) breaks the trust posture for no gain a 15-minute tick does not give. (d) is a dependency and a second truth |
| Where progress lives | (a) a local watermark; (b) the store's coverage, journal as cache | (b), the store's proposal: it survives exactly as far as the memory it describes (STO-12) |
| Who said it | (a) v1's word overlap, tuned; (b) exact quotes checked by code; (c) segment ids; (d) a second model judging entailment | (b), with (c) as the measured fallback. (a) cannot be made language-neutral. (d) doubles calls to guard a risk the fidelity suite can measure |
| Calls per episode | (a) one call, operations on all three products; (b) extract, then reconcile in a second call | (a). (b) is the audit's direction for UPM-10, but what made v1's one call a problem was full-state output and one verdict. Reconciliation is code where it can be, and a duplicate is support, not an error |
| When the fast pass runs | (a) every turn; (b) at boundaries and after quiet; (c) hours later, as the hosts' own memories do | (b). (a) is ruled out and is where quota complaints come from. (c) fails the product's own bar: the next chat starts stale (the brief audit's F2) |
| Consolidation | (a) none; (b) code housekeeping, model review gated on an audit; (c) a standing model consolidation on a candidate copy | (b). One vendor dropped model reconciliation because it destroyed context and another rejected offline curation for cost; three shipped it in 2026 with no outcome numbers. Code does the deterministic part now; the model part must earn its place |
| Unit of working memory | (a) branch threads, with a fix for work outside git; (b) workstreams, bound by branch where a branch exists and by a bounded model choice elsewhere; (c) topic clusters chosen by a model everywhere | (b). (c) puts a model in charge of structure everywhere, against principle B |
| Model policy | (a) the chat's model at the highest effort; (b) a fixed small model; (c) a measured default with one escalation | (c) |
| API key | (a) plan login only; (b) the host CLI on a key; (c) eva-md's own HTTP client | (b). (c) adds a network client and a credential path to a product whose trust posture is that it has neither |
| System pages | (a) freeze and measure; (b) remove; (c) rebuild with a refresh lifecycle | (a). (c) invests in the category the evidence is against. (b) deletes a person's files' purpose before measuring |

---

## 14. Contracts

| Id | With | Direction | One line |
| --- | --- | --- | --- |
| C-D1 | ledger | promise | The worker runs `ledger.Ingest` on every wake, outside the worker lock, so it starts within a second and never waits for a model call; single-flight is the session lock's |
| C-D2 | ledger | need | Settled turns and ended sessions are readable from the session record; the wake's session name is only a hint |
| C-D3 | ledger, store | promise | Progress is `(session, seq range, outcome)` in the store's coverage; v1 watermarks arrive already as coverage |
| C-D4 | ledger | need | `ledger.Episodes`, `ledger.Render` (deterministic, with speaker label, taint and cut lines), `ledger.Verify(event, quote)` with NFC and white-space normalization only, `ledger.Excerpt` |
| C-D5 | ledger | promise | Only `pv` and `taint` decide a tag (5.5); segments are not read until an evaluation says they help |
| D-L1 | ledger | need | Each event's context carries branch, captured head and whether the branch is the project's default; a failed tool record carries its status and digest; pasted parts are spans; a fork or resume names its parent |
| D-L2 | ledger | need | Remember, correct, confirm and forget are ledger events with typed arguments, so a replay applies them again (the store's S-L3) |
| D-L3 | ledger | promise | `work.Holding(session)` answers a forget; a forget removes `pending/` batches of that session and the worker covers forgotten ranges as `skipped: forgotten` |
| D-S1 | store | need | `store.Apply(Txn)` with per-operation outcomes and exactly-once per pass id; `store.Plan` as the review's lint gate; `store.Coverage` as the truth; a refused operation never holds coverage back (the store's S-D1, S-D3, S-D5, S-D6) |
| D-S2 | store | promise | This area owns the table of kinds, working kinds, states, tags and the grammar of `why`, `steps`, the episode's trail and notes, versioned with the schema (S-D4). It has no kind `rule`: a v1 rule is a `constraint` |
| D-S3 | store | promise | Derive checks meaning before `Apply` and never writes a file, an id, a date or a path (S-D2) |
| D-S4 | store | need | A protected item returns `locked` or `verified` per operation; derive then submits a dispute beside it. A merge report of what a merge dropped comes back the same way (S-D7) |
| D-S5 | store | need | Workstream ids can be minted from a key (project and branch), so two machines that meet one branch agree |
| D-R1 | deliver | need | One retrieval core serves recall and the choice of related items at write time: `retrieve.Related(project, text, paths, k)` |
| D-R2 | deliver | promise | Every active item carries kind, tag, scope, provisional, anchors, cues, learned, confirmed, support, the disputed and stale flags, and its excerpts; a standing instruction can always be shown with the user's own quote |
| D-R3 | deliver | need | Use counts from delivery records (delivered, validated, contradicted) in local state, for archive by disuse |
| D-R4 | deliver | promise | The write tools call `organize.Check` and `store.Apply` synchronously; a remember is `stated` only when a typed span in that turn carries it, otherwise a candidate |
| D-R5 | deliver | promise | Freshness per session and workstream: organized through which seq, turns pending, last pass, so a brief can say "pending" truthfully |
| D-R6 | deliver | need | System pointers are off by default and exist as an evaluation arm |
| D-H1 | hosts | need | The OS tick calls `eva-md work --tick`; without it the MCP server does, every 10 minutes while it lives |
| D-H2 | hosts | need | The hook declaration includes the wake events once, with capture's; the recorded CLI path and version per host; upgrade tells the worker the binary and contract version changed |
| D-H3 | hosts | promise | Typed conditions with a remediation class: `organize.sign_in{host}` (one plain ask), `organize.paused{host, since}`, `organize.backlog{oldest}`, `organize.refused{n}`, `organize.budget_spent` (all automatic) |
| D-H4 | hosts | promise | `work.Cost(day, host)` and `work.Status()`; both content-free |
| D-H5 | hosts | need | Install states once, as a statement, that organizing spends the user's plan on a small model and how to turn it off |
| D-E1 | evaluate | need | The fidelity suite: fact-level, value-exact, black-box, with held-out scripts, translated twins, a poisoning corpus and an aging run; a results store with baselines |
| D-E2 | evaluate | promise | `organize.Derive(ledger fixture, runner) -> store` runs the whole area as a library with a fake, a recorded or a live runner; every refusal has a code; the journal has every counter |
| D-E3 | evaluate | need | The corpus audit (near-duplicates, disputes, stale in force, promotion yield, items the repository already states) decides whether the review tier is built |
| D-E4 | evaluate | need | The branch-switching suite has a default-branch variant that scores workstream binding |

Differences from the sibling drafts, for the lead to reconcile: the store draft maps rules pages to "kind rule" and gives a workstream body "a short maintained paragraph" and an episode a `Summary`; this design has no `rule` kind, makes the paragraph one cited status line, and makes the summary the code-built trail. The store draft's workstream states are started, active, merged, archived; this design's are active, paused, merged, abandoned, archived.

---

## 15. Where the lead's draft is wrong or over-built

1. **"Hooks only enqueue."** There should be no queue to trust. Pending work is the ledger minus coverage; a wake is a hint. And a wake must run ingest itself when the worker is busy, or capture's top-priority job waits behind a model call.
2. **"A fast cheap pass soon after a turn settles."** Not per turn: that is the ruled-out "model call on every turn" and the surest way to be noticed in a quota meter. The freshest layer is the ledger's manifest, which needs no model. The fast pass runs per settled episode.
3. **"A periodic consolidation on a candidate copy with a lint gate" as a standing tier.** Folding merged branches, retiring, promoting what recurs and merging exact duplicates are deterministic and belong in code, every tick. What is left for a model is near-duplicates and disputes, and that should be built only when the corpus audit shows them. No candidate copy is needed when the model returns operations: `store.Plan` is the gate.
4. **"Acceptance per product (state, log, rules, knowledge)."** Three products. A rule is an item with the user's tag; a separate rules store is how v1 came to hold constraints in two places.
5. **"Store only what the repository, instruction files and host memory cannot say."** Host memory should not block storing: it is one host's, on one machine. It should stop delivery where the host already loads it.
6. **"Promote what recurs."** Only when recurrence is independent: two sessions, and never a restatement of what eva-md itself delivered. Otherwise memory promotes its own echo.
7. **"Backends (host headless CLI, in-session model call where a mod runs, API key)."** Drop the mod backend from v2. Make the API-key backend the same CLI with a key, not a client of eva-md's own.
8. **"Procedures exportable as host skills."** Self-written skills measured 8 to 11 points worse on both hosts. No export in v2 from this area; a reviewed export can follow an evaluation.
9. **"Evidence as verbatim spans that code checks by exact match."** Right, with two additions: the normalization must be stated (white space and NFC, nothing else), and there must be a measured fallback, because a small model's quote drift is unknown until DV-03.
10. **"Dead letters replayed after an upgrade."** Also after a host CLI change and a model policy change, each for the reasons it could fix, inside a budget share, or the first upgrade spends a day's quota on old refusals.
11. **"A host-level circuit breaker."** It needs a control: a known-good fixture as the probe. Without one, a breaker cannot tell a broken host from a bad episode, and blame creeps back to the content.
12. **System pages.** The draft's warning is right, and the conclusion should be firmer: freeze now, measure, and decline the audit's refresh-queue direction.
13. **Workstreams "in place of branch-keyed threads".** Keep the branch as the binding wherever a non-default branch exists. It is the one part of v1's unit that is right and it needs no model.

---

## 16. Owner decisions

| Id | Question | Options | Recommendation | Why | If no |
| --- | --- | --- | --- | --- | --- |
| DV-D1 (lead's D-A) | May one OS-scheduled, short-lived tick wake the worker every 15 minutes? Reverses "no sleeping jobs" | tick; host-driven only | Tick | It is what organizes a chat that never ends, and what still runs when hooks do not | 3.8's fallback: hook wakes and the MCP server's 10-minute call. Cost: nothing runs with every chat closed; a chat with no end waits for the next activity |
| DV-D2 (lead's D-D) | May organizing use a small model at low effort chosen from a measured frontier, not the chat's model at the highest effort? Reopens decisions 5 and 17 | measured small default with one escalation; the chat's model at a lower effort; as v1 | Measured small default | Quota is the loudest complaint on both hosts, and the hosts' own memory writers use a small model at low effort | The chat's model, default effort low, the token budget kept. Cost: several times the tokens per chat (to be measured) and slower passes |
| DV-D3 | May the host CLI run on the user's own API key as an opt-in backend? | opt-in; never | Opt-in | It keeps organizing alive if headless runs leave the plan login, and serves setups that bill through a key or a cloud provider | Plan login only. Cost: if `claude -p` loses the plan login, Claude-side organizing stops until a release adapts; the brief still works |
| DV-D4 | May an agent's proposal the user accepted (a "yes", a dialog choice, a plan approval) become a durable decision, with both spans kept? | yes for decisions and procedures, never for standing rules; typed words only | Yes, as stated | It is how most decisions are made in agentic coding, and both spans are in the record | Typed words only. Cost: such decisions stay as workstream questions and notes, as in v1 |
| DV-D5 | May eva-md hold back an item whose cited code is gone, and record a contradicting observation as a dispute, without the user's words? It never changes a user-tagged item | hold back and dispute; label only | Hold back and dispute | A stale fact shown as current is the commonest complaint about memory tools | Label only. Cost: facts about deleted code keep arriving unasked, with a date |
| DV-D6 | System pages: freeze and measure, remove, or rebuild? | freeze; remove; rebuild with refresh | Freeze | The evidence is against model-written overviews; nothing has measured these helping | Rebuild as a view over cited, anchored items for a system's paths. Cost: a large package and an evaluation, for a feature the evidence does not favour |
| DV-D7 (lead's D-M) | May a person run an opt-in, budgeted replay of the v2 contract over the existing local record, after seeing a token estimate? Touches "history backfill" | opt-in command; never | Opt-in command | It recovers what v1 refused or skipped and gives migrated items verified evidence | Never. Cost: v2's memory starts at cut-over; what v1 passed over stays only in search |
| DV-D8 | Build the model review tier now, or only if the corpus audit shows near-duplicates and disputes that code cannot resolve? | gated on the audit; now; never | Gated | Two vendors walked back from model reconciliation; none has published an outcome for it | Now. Cost: a large package and up to a fifth of the budget, before anything shows it is needed |

---

## 17. Risks

1. **Small models and exact quotes.** Yield may be low if quotes drift. The repair round, the segment-id fallback and the escalation step are the answers, and DV-03 measures before Stage 2 builds on any of them.
2. **A faithful quote under an unfaithful statement.** The tag is right and the sentence is wrong. Grounding, showing the user's quote with standing instructions, and value-exact scoring bound it; none removes it.
3. **The headless path moves.** The plan login for `claude -p` has been re-billed, paused and may default to a mode that cannot use it. The probe, the breaker, the opt-in key backend and a brief that needs no model limit the damage to stale derived memory.
4. **Workstream binding on the default branch** can put state in the wrong place. The choice is bounded to shown handles, measured by a suite, and has a model-free fallback.
5. **The tick is declined, or blocked on a managed machine.** Freshness falls back to activity; nothing else changes.
6. **Replay storms.** An upgrade could make a week of refused ranges eligible at once. The 30% share and newest-first order bound it.
7. **Repair doubles calls** on a host that does not cache the shared prefix. Measured; then limited to user-tagged refusals.
8. **Lessons copy mistakes.** An agent follows a stored experience closely, errors included. A lesson needs a failed tool record, is never user-tagged, decays by disuse, and how often it is delivered is the deliver area's choice.
9. **Migration changes labels.** v1 `observed` items that rested on a report become `suggested`. They stay active; the before-and-after of what a brief shows is checked on the fixture wiki.
10. **Over-building.** This design has more parts than v1's update. Stage order is the guard: the fast pass with state, log and user-tagged items ships first; candidates, code-checked facts, housekeeping beyond folding, and review each wait for a measure.
11. **Memory text in `pending/`.** A validated batch sits on disk for seconds. It is in the 0600 state folder, removed at commit and by a forget.

---

## 18. Work packages

Stages: 0 ground truth, 1 foundations, 2 core rebuilds, 3 memory quality, 4 reach and sync, 5 proof. Sizes: S up to two days of one agent, M up to a week, L more. "Live" steps need the owner to run them or to give leave, because they spend model quota.

| Id | Stage | Size | Outcome | Scope | Depends on | Acceptance, and the measure that must move |
| --- | --- | --- | --- | --- | --- | --- |
| DV-01 | 0 | S | v1's run log says what the validator and the triggers cost | `internal/update/runlog.go` and a read-only `eva-md status --updates`: outcomes and reasons, refusals by rule, tokens, duration, boundary-to-saved, busy exits with their session; record the rule and place of a whole-answer rejection (today dropped at `pass.go:401-403`). No behaviour change otherwise | none | On a fixture run log the report prints exact counts and no text (a test scans the output); on the real install it answers field questions 1 to 4. Measure: refusal-by-rule and time-to-memory baselines exist |
| DV-02 | 0 | M | v1's update has met real models, and their output is recorded | owner-run `doctor --live` and `cmd/live-accuracy` on both hosts, extended to carry rules and knowledge in its batches; a recorder that saves envelopes and event streams, scrubbed, under `testdata/<host>/<cli version>/headless/` | owner's leave | Every row of section 11 that concerns today's command has a recorded answer; fixtures hold only made-up values. Measure: v1 `fact_recall`, tokens and seconds per pass exist for the chat's model at the highest effort |
| DV-03 | 0 | M | The contract's open bets are settled by measurement | a throwaway prototype of contract v2 (prompt, schema, quote check) run black-box on the fact scripts and their translated twins, on both hosts, at three model tiers and two efforts; quotes against segment ids; repair on and off; cached tokens of a repair call | DV-02 | A frontier table in `docs/v2/` with fidelity, tokens and seconds per row; a decision record naming the default row, the evidence form and the repair policy by the rules of 5.4, 5.8 and 8.3. Measure: `quote_miss`, `fact_recall`, `wrong_write` per row |
| DV-10 | 1 | M | Specs a team can build from | per-component specs: the work ledger and job states, contract v2 with its generated field and refusal tables, the tag and kind tables, the lattice, lifecycle; decision records for DV-D1 to DV-D8 | DV-03, owner decisions | A reviewer finds, for every refusal code, its prompt sentence, its test and its repairability; the guarantees are a data table. Measure: hidden rules (a code with no prompt sentence), 0 |
| DV-11 | 1 | M | A runner with a probe and typed outcomes | `internal/modelrun`: evolve the `hostCLI` seam; Claude stream reader; Codex reader with a positive tool list; probe cache; policy table; key-command billing. `process.go` unchanged | DV-02 | v1's runner tests pass, ported; recorded fixtures classify correctly; a fixture with an allowed tool call is `tool_used`; an unknown event type is `host`; the answering model is recorded; a killed run reports usage seen. Measure: misclassified recorded outcomes, 0 |
| DV-12 | 1 | M | One worker, no lost wake | `internal/work`: locks and the waiter handoff, wake marks, journal, snapshot, priorities, the registration interface, `eva-md work`; until the fast pass exists it runs v1's update and maintain as registered jobs, so lost triggers stop at once; the no-tick fallback | none | A test fires 50 wakes during a held job and every session is processed, with at most 2 processes (a seam counts them); a kill between start and finish is journaled as interrupted and counted in the budget; the journal holds no text. Measure: `lost_wakes` 0 on generated schedules |
| DV-13 | 1 | S | A cost ledger and a token budget | usage records, weighted budget, `work.Cost`, the weekly counters | DV-12 | A fixture journal gives exact totals with cached input apart; a job over the remaining budget is deferred, not failed. Measure: tokens per organized chat reported |
| DV-20 | 2 | L | Episodes and a cursor | `internal/organize/episode`: builder over `ledger.Episodes`, carried context, legend, cursor from coverage, dry run. Deletes the prefix scan and markers | ledger LG-10, LG-21 | Golden renders; a property test over random sessions: every released event is in exactly one episode, no cut falls inside a turn that fits, typed text under 32 KB is whole; the dry run is byte-stable. Measure: mid-turn cuts on fixtures, 0 |
| DV-21 | 2 | L | Extraction contract v2 and its checker | `internal/organize/contract`: the generated schema and prompt, the checker of 5.7, tags from `pv` and taint, acceptance per product, the repair round, `Guarantees` v2. Deletes the word-overlap code | DV-10, DV-20, ledger LG-13 | v1's S1 and S4 tests pass in their new form; the v1 test that lets an observation supersede a stated fact now expects a dispute; poisoning fixtures give `stored_with_authority` 0; translated twins are within 5 points; live, at the default row: `fact_recall` at least 0.90, `wrong_write` at most 0.02, `misattributed` 0. Measures: `honest_refused` at most 5%; `accepted_captured` at least 80% |
| DV-22 | 2 | M | Failures that cost little and lose nothing | outcome classes to consequences, the control rule, the breaker with the live fixture as probe, timeout adaptation, refused coverage, replay triggers and its budget share | DV-11, DV-12 | An unknown Codex event type across 5 sessions opens the breaker after 3 calls, blames no session and covers no range; a CLI version change probes and resumes; a timeout's next attempt is smaller and lower. Measures: `ranges_unaccounted` 0 in the aging suite; calls burned under a simulated host break at most 3 plus probes |
| DV-23 | 2 | M | The memory semantics, with the store | the kind, tag and state tables; the lattice; provisional items; workstream binding and transitions; state ordering by evidence time; the hardened git check for merged branches | store ST-05, DV-21 | Table-driven tests of every cell of 6.4; two machines' sessions on one workstream neither clobber state nor collide; a merged branch's decisions are project items and an abandoned branch's are retired. Measure: `stale_in_force` at most 0.02 on the correction scripts |
| DV-24 | 2 | M | The fast pass, end to end | ripeness, gates, the job body, `pending/` batches, freshness facts, the P1 write path's use of the checker | DV-20 to DV-23, store ST-05 | A chat ends and one commit holds its log, state and items with coverage; a second run is a no-op; a kill after validation costs no second call. Measures: `end_to_saved` p50 at most 90 s and p90 at most 5 min, live; tokens per chat at most a fifth of DV-02's baseline |
| DV-25 | 2 | M | v1 state and meaning carried over | the mapping of section 12 for the store's migrator; coverage from v1 watermarks; the run-log baseline; no model call | store ST-07, ledger LG-25 | On the fixture wiki and inbox: every v1 decision, rule and knowledge statement is an item with the same text and the mapped tag; nothing is organized twice; a second run changes nothing. Measure: statements lost in migration, 0 |
| DV-26 | 2 | S | The v1 control plane is gone | delete the trigger thresholds, the sweep, the run-log state, the quarantine skip and the `maintain` job; `eva-md update` as an alias; AGENTS.md's restricted-command rule, README and the docs updated in the same change | DV-24 | Hook benchmark figures do not rise; `scripts/linux-check.sh` passes; no non-test reader of the v1 run log remains |
| DV-30 | 3 | M | Housekeeping | H1 to H8, drift for item anchors through `internal/coderef`, use counts from deliver | DV-23, deliver's delivery records | In a 200-session aging run with canned answers: exact duplicates 0, no user-tagged item archived or retired by code, candidates promoted at two independent supports and never by an echo. Measure: duplicate rate at most 1% |
| DV-31 | 3 | M | Facts, procedures and lessons with evidence code can check | procedure steps matched to run records; lessons tied to failed tool records; fact anchors checked at the captured head | DV-24, ledger LG-23 | A procedure whose commands never ran stays a candidate; a lesson without a failure record is a note; an anchored fact whose path is absent at the captured head is refused. Measure: corrections per task on the hidden-decision suite fall against memory off |
| DV-32 | 3 | S | The corpus says whether a review tier is needed | content-free counters: near-equal clusters, open disputes, candidates by age, promotion yield, refusals by code | DV-30, evaluate's corpus audit | The counters match a seeded fixture store; a decision record applies DV-D8's rule to a real week's figures |
| DV-33 | 3 | L | A model review, only if DV-32 says so | clusters, the review call, the `merge` operation, `store.Plan` as the gate | DV-32, DV-D8 | A seeded review that drops a user constraint is blocked; an undo restores; on the aging run near-equal active items fall under 2% with no user-tagged item changed. Measure: review tokens at most a fifth of the budget |
| DV-34 | 3 | M | Opt-in replay with an estimate | `eva-md work --replay --estimate`, on the dead-letter machinery | DV-22, DV-D7 | The estimate is within 30% of a fake runner's usage; newest first; stops at the budget; a replay over migrated items adds support and no duplicate |
| DV-35 | 3 | S | System pages frozen | pointers off by default as an evaluation arm; pages marked as model-written reference; no new sibling relations from an agent's list | deliver, DV-D6 | With the default config no system pointer is injected; `show` and search still work; existing relations are kept and marked unconfirmed |
| DV-40 | 4 | S | Organizing on more than one machine | reconcile to coverage after a sync, a merge report turned into disputes, workstream ids from a shared key | store ST-09 | Two simulated machines organize interleaved sessions of one branch and converge on one workstream; a range one machine covered is never run by the other |
| DV-50 | 5 | M | A week of proof and the release gates | the Level 0 gates in CI on recorded answers; a week on the real install; live fidelity on both hosts' current versions | DV-24, DV-30 | CI fails on a seeded misattribution and a seeded unaccounted range; the week's report meets section 1's bars or records which did not and why. Measures: all of section 1 |

Order that matters: DV-01 to DV-03 before the scope of DV-21 is fixed; DV-12 can ship on v1 alone and ends lost triggers early; DV-21 before DV-23, because tags decide the lattice; DV-33 only on DV-32's evidence.

---

## 19. What a field report from the real install should answer for this area

In general terms, counts only, nothing quoted.

1. The update run log over the install's life: passes by outcome and reason for each host; validator refusals by rule; rules and knowledge added against refused; whole-answer rejections; quarantines and skips by reason.
2. For passes that called a model: duration, tokens in and out, calls a pass, the chunk's size, the model and the effort asked and used, and how long from the chunk's newest entry to the saved page (median and the slow tail).
3. How often a job found another running and exited, and how long those sessions then waited.
4. Sessions with a backlog now, the oldest one's age, and how many short chats have never been processed.
5. How chats end: the share of sessions with an end event, with a compaction, left open; the share on the default branch, on other branches and outside any repository; how many thread pages belong to branches that are merged or gone.
6. How decisions are made: how many typed prompts are a few words long and follow an assistant's proposal, and how often the hosts' question, plan-approval and delegation tools appear in the hosts' own records.
7. Whether `doctor --live` has run on each host and what it said; whether a headless run on the plan login works from a detached job; which effort levels each CLI accepted; any model or effort fallback notes.
8. Whether the plan's limits were hit in the period, and model calls a day against the cap.
9. The wiki's items by kind, tag and state; rules; decisions per thread and whether any thread is at its cap; statements that look like duplicates by a simple normalized match; how many system pages exist and how often a tool record shows one being opened.
10. Whether prompts are written in a language other than English, as yes or no.
11. Whether a per-user scheduled job is allowed on the machine, and whether managed settings are present.
12. Host CLI versions, and whether either changed during the period with a change in update outcomes around that date.
